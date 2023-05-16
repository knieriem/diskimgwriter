case $GOARCH in
	386)
		toolpfx=i686
		O=8
		;;
	amd64)
		toolpfx=x86_64
		O=6
		;;
esac

bin=tmp

awk '{print "s/@" $1 "@/" $2 "/"}' <VERSION > ,,version.sed

tcs -t windows-1252 < windows/rc | sed -f ,,version.sed > ,,$bin.rc
rm -f ,,version.sed

$toolpfx-w64-mingw32-windres -o $1 ,,$bin.rc
rm -f ,,$bin.rc
