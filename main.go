//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/exp/slices"
	"golang.org/x/sys/windows"

	"code.cloudfoundry.org/bytefmt"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/yusufpapurcu/wmi"

	"github.com/knieriem/diskimgwriter/internal/sys"
)

var (
	dryRun    = flag.Bool("n", false, "do not perform actual writes")
	blockSize = flag.Uint("bs", 64, "block size")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options] <image>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "options:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	err := do(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		fmt.Println("Press enter to exit ...")
		fmt.Scanln()
		os.Exit(1)
	}
}

func do(imageFilename string) error {
	disks, err := lookupRemovableDrives()
	if err != nil {
		return err
	}
	if len(disks) == 0 {
		return fmt.Errorf("no removable drives found")
	}

	r, err := setupImageReader(imageFilename)
	if err != nil {
		return err
	}
	defer r.Close()

	iDisk, err := userChoice(disks)
	if err != nil {
		return err
	}

	disk := disks[iDisk]
	if *dryRun {
		fmt.Printf("\nDRY RUN (not actually writing to disk)\n")
	}
	fmt.Printf(`
Are you sure that %q (index: %d)
shall be overwritten with
	%q [y/N]? `,
		disk.drive.Model, iDisk, imageFilename)
	var confirm string
	fmt.Scanln(&confirm)

	if confirm != "y" && confirm != "Y" {
		return fmt.Errorf("aborted")
	}

	return disk.copyImageFrom(r)
}

func lookupRemovableDrives() ([]*Disk, error) {
	var drives []Win32_DiskDrive
	//	wmi.DefaultClient.AllowMissingFields = true
	q := wmi.CreateQuery(&drives, "WHERE (MediaType = 'External hard disk media' or MediaType = 'Removable Media')")
	err := wmi.Query(q, &drives)
	if err != nil {
		return nil, err
	}
	if len(drives) == 0 {
		return nil, nil
	}

	disks := make([]*Disk, 0, len(drives))
	for i := range drives {
		d := &Disk{drive: &drives[i]}
		err := d.AssociateVolumes()
		if err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, nil
}

type Disk struct {
	drive *Win32_DiskDrive

	Volumes     []*Volume
	volumeNames []string
}

type Volume struct {
	Name     string
	Filename string

	f      *os.File
	locked bool
}

func (d *Disk) AssociateVolumes() error {

	partList, err := d.partitions()
	if err != nil {
		return err
	}

	err = walkVolumes(partList, func(volName string) error {
		filename := `\\.\` + volName
		d.Volumes = append(d.Volumes, &Volume{Name: volName, Filename: filename})
		return nil
	})
	if err != nil {
		return err
	}

	names := make([]string, 0, len(d.Volumes))
	for _, v := range d.Volumes {
		names = append(names, v.Name)
	}
	d.volumeNames = names

	// Ensure that the volume does not extend on any of the other disks' partitions
	otherPartList, err := otherPartitions(partList)
	if err != nil {
		return err
	}

	return walkVolumes(otherPartList, func(volName string) error {
		if slices.Contains(names, volName) {
			return fmt.Errorf("volume %s extends itself over multiple disks", volName)
		}
		return nil
	})
}

func walkVolumes(partList []Win32_DiskPartition, fn func(volName string) error) error {
	for i := range partList {
		var volumes []Win32_LogicalDisk
		part := &partList[i]
		q := fmt.Sprintf("Associators of {Win32_DiskPartition.DeviceID='%s'} where AssocClass=Win32_LogicalDiskToPartition", part.DeviceID)
		err := wmi.Query(q, &volumes)
		if err != nil {
			return err
		}
		for i := range volumes {
			vol := &volumes[i]
			err := fn(vol.Name)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Disk) partitions() ([]Win32_DiskPartition, error) {
	var partList []Win32_DiskPartition

	q := fmt.Sprintf("Associators of {Win32_DiskDrive.DeviceID='%s'} where AssocClass=Win32_DiskDriveToDiskPartition", d.drive.DeviceID)
	err := wmi.Query(q, &partList)
	if err != nil {
		return nil, err
	}
	if len(partList) != int(d.drive.Partitions) {
		return nil, fmt.Errorf("partition count mismatch")
	}
	return partList, nil
}

func otherPartitions(diskPartList []Win32_DiskPartition) ([]Win32_DiskPartition, error) {
	var otherPartList []Win32_DiskPartition

	q := wmi.CreateQuery(&otherPartList, "")
	err := wmi.Query(q, &otherPartList)
	if err != nil {
		return nil, err
	}

L:
	// remove diskPartList from otherPartList
	for i := range diskPartList {
		part := &diskPartList[i]
		for j := range otherPartList {
			otherPart := &otherPartList[j]
			if part.DeviceID == otherPart.DeviceID {
				otherPartList = append(otherPartList[:j], otherPartList[j+1:]...)
				continue L
			}
		}
		return nil, fmt.Errorf("inconsistent partition information")
	}
	return otherPartList, nil
}

func (d *Disk) LockDismountVolumes() error {
	var errs []error

	for _, v := range d.Volumes {
		err := v.lockDismount()
		if err != nil {
			errs = append(errs, err)
			break
		}
	}
	err := errors.Join(errs...)
	if err != nil {
		d.UnlockVolumes()
		return err
	}
	return nil
}

func (d *Disk) UnlockVolumes() error {
	for _, v := range d.Volumes {
		v.unlock()
	}
	return nil
}

func (v *Volume) lockDismount() error {
	f, err := os.OpenFile(v.Filename, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	v.f = f
	err = v.deviceIOCtl(sys.FSCTL_LOCK_VOLUME)
	if err != nil {
		return err
	}
	v.locked = true

	return v.deviceIOCtl(sys.FSCTL_DISMOUNT_VOLUME)
}

func (v *Volume) unlock() error {
	defer v.f.Close()
	err := v.deviceIOCtl(sys.FSCTL_UNLOCK_VOLUME)
	if err != nil {
		return err
	}
	return nil
}

func (v *Volume) deviceIOCtl(code uint32) error {
	var bytesReturned uint32
	return windows.DeviceIoControl(windows.Handle(v.f.Fd()), code, nil, 0, nil, 0, &bytesReturned, nil)
}

func (d *Disk) copyImageFrom(r io.Reader) error {
	err := d.LockDismountVolumes()
	if err != nil {
		return err
	}
	defer d.UnlockVolumes()

	diskname := d.drive.DeviceID
	fmt.Printf("\nWriting image to %s ...\n", diskname)
	f, err := os.OpenFile(diskname, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	w := io.MultiWriter(f, h)
	if *dryRun {
		w = h
	}

	var buf = make([]byte, *blockSize*1024)
	var nRead uint64

	t0 := time.Now()
	printStats := func() {
		elapsed := time.Since(t0)
		fmt.Printf("\r%7s %7s/s %8v  ",
			bytefmt.ByteSize(nRead),
			bytefmt.ByteSize(uint64(float64(nRead)/elapsed.Seconds())),
			elapsed.Round(time.Second))
	}
	for {
		n, err := io.ReadFull(r, buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			if err != io.ErrUnexpectedEOF {
				return err
			}
		}
		if n == 0 {
			break
		}
		if !*dryRun {
			nw, err := w.Write(buf[:n])
			if err != nil {
				log.Printf("write error at pos %s: %v", bytefmt.ByteSize(nRead), err)
				return err
			}
			if nw != n {
				return fmt.Errorf("short write")
			}
		}

		nRead += uint64(n)
		if nRead&(0x0080_0000-1) == 0 {
			printStats()
		}
	}
	printStats()
	fmt.Println()
	if *dryRun {
		printDoneMsg()
		return nil
	}
	wSum := h.Sum(nil)

	fmt.Printf("\nVerifying written image ...\n")
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	buf = make([]byte, 1024*1024)
	h.Reset()
	t0 = time.Now()
	_, err = io.CopyBuffer(h, io.LimitReader(f, int64(nRead)), buf)
	if err != nil {
		return err
	}
	printStats()
	fmt.Println()
	fSum := h.Sum(nil)
	if !bytes.Equal(wSum, fSum) {
		return fmt.Errorf("verification failed")
	}

	printDoneMsg()
	return nil
}

func printDoneMsg() {
	fmt.Println(`
Done.

You may eject the drive now,
then press enter to exit the program ...`)
	fmt.Scanln()

}

type Win32_DiskDrive struct {
	Name           string
	DeviceID       string
	Index          uint32
	Model          string
	MediaType      string
	BytesPerSector uint32
	Size           uint64
	Partitions     uint32
	Manufacturer   string
	MaxBlockSize   uint64
	SerialNumber   string
}

type Win32_DiskPartition struct {
	DeviceID string
	Name     string
}

type Win32_LogicalDisk struct {
	Name     string
	DeviceID string
}

func userChoice(disks []*Disk) (int, error) {
	fmt.Printf("Removable drives found:\n\n")
	for i, disk := range disks {
		fmt.Printf("\t%d:\t%q\n", i, disk.drive.Model)
		fmt.Printf("\t\tSize: %v\n", bytefmt.ByteSize(disk.drive.Size))
		fmt.Printf("\t\tVolumes: %s\n\n", strings.Join(disk.volumeNames, " "))
	}

	indexRange := "(0)"
	if len(disks) > 1 {
		indexRange = fmt.Sprintf("(0 .. %d)", len(disks)-1)
	}
	fmt.Printf("\nEnter the index of the drive to be written to [%s]: ", indexRange)

	var iDisk uint
	fmt.Scanln(&iDisk)

	if iDisk >= uint(len(disks)) {
		return 0, fmt.Errorf("invalid drive index %d", iDisk)
	}
	return int(iDisk), nil
}

func setupImageReader(filename string) (io.ReadCloser, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(filename)
	switch ext {
	case ".img":
		return f, nil
	case ".zst":
		r, err := zstd.NewReader(f)
		if err != nil {
			return nil, err
		}
		return &readCloser{
			Reader: r,
			close: func() error {
				r.Close()
				return nil
			},
		}, nil
	case ".br":
		r := brotli.NewReader(f)
		return &readCloser{Reader: r, close: f.Close}, nil
	}
	return nil, fmt.Errorf("unknown image file extension %q", ext)
}

type readCloser struct {
	io.Reader
	close func() error
}

func (rc *readCloser) Close() error {
	return rc.close()
}
