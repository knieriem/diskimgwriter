RESOFILES=\
	res_windows_amd64.syso\

all:	diskwriter.exe

diskwriter.exe: $(RESOFILES)
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '-s -w'

clean:
	rm -f $(RESOFILES)
	rm -f windows/icon.png
	rm -f windows/icon.ico
	GOOS=windows go clean

res_windows_%.syso: windows/icon.ico
	GOOS=windows GOARCH=$* sh windows/windres.sh $@

windows/%.ico: windows/%.png
	convert $< -define icon:auto-resize=256,128,64,48,32,16 $@

windows/%.png: windows/%.svg
	inkscape -w 256 -h 256 $< -o $@

.PHONY: all clean
