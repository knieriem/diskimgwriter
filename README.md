# Diskimgwriter

Diskimgwriter is a command line tool to be run on Windows
to write whole disk images,
like an RPi image consisting of multiple partitions,
to a removable disk drive like a USB thumb drive or an SD card.

It also works if the disk drive already contains multiple partitions,
i.e. got multiple drive letters (volumes) assigned to it by the operating system,
which appears to be a problem with other image writers that offer a choice
based on drive letters.


## Run diskimgwriter.exe

Diskimgwriter can be run by entering

	diskimgwriter file.img

in a command interpreter window,
or by dragging `file.img` onto diskimgwriter's icon.
The program, as configured by a setting in the [manifest file],
will request adminstrator privileges to get access to the physical drive.
A [UAC] consent promp will show up that needs to be confirmed with _yes_.

A console window will open,
presenting a menu for selecting a disk drive,
and giving information about progress:

```console
Removable drives found:

        0:      " USB  SanDisk 3.2Gen1 USB Device"
                Size: 28.7G
                Volumes: D: E:

        1:      "JetFlash Transcend 16GB USB Device"
                Size: 14.4G
                Volumes:

        2:      "Generic USB  SD Reader USB Device"
                Size: 7.4G
                Volumes: H:


Enter the index of the drive to be written to [(0 .. 2)]: 0

Are you sure that " USB  SanDisk 3.2Gen1 USB Device" (index: 0)
shall be overwritten with
        "rpi.img.zst" [y/N]? y

Writing image to \\.\PHYSICALDRIVE5 ...
     3G   12.3M/s    4m13s

Verifying written image ...
     3G   91.9M/s      34s

Done.

You may eject the drive now,
then press enter to exit the program ...
```


## Supported image compression algorithms

In addition to plain, uncompressed images,
diskimgwriter supports brotli and Zstandard compressed images,
which must have the filename extension `.img.br` resp. `.img.zst`.

[manifest file]: ./windows/manifest
[UAC]: https://learn.microsoft.com/en-us/windows/security/identity-protection/user-account-control/how-user-account-control-works
