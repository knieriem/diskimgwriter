W=.

OSARCH=\
	windows_386\
	windows_amd64\

ITEMS=\
	const\

ZFILES=\
	$(foreach f,$(ITEMS),$(foreach a,$(OSARCH),z$(f)_$a.go))\

all:	$(ZFILES)

clean:
	rm -rf _obj	# created by cgo

zclean:
	rm -f z*.go

$(foreach f,$(ITEMS),z$(f)_windows_%.go):	windows/c.h windows/const $W/gen.sh
	GOOS=windows GOARCH=$* PKG=sys ZDIR=$W sh $W/gen.sh
