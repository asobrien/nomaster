.PHONY: all

SOURCE = github.com/asobrien/nomaster
GOPATH := ${HOME}/.gopath
export GOPATH

# Requires gox
# Inspired by: http://spf13.com/post/cross-compiling-go/

all:
	@echo
	@echo "Useful help message here ..."
	@echo "You probably just want to run:"
	@echo "    make shipit"
	@echo

new-build:
	rm -rf ~/.gopath
	mkdir ~/.gopath

install-gox:
	export GOPATH=$(GOPATH)
	go get github.com/mitchellh/gox

xcompile: new-build install-gox
	rm -rf build
	mkdir build
	go get $(SOURCE)
	cd build && \
	${GOPATH}/bin/gox $(SOURCE)

hash:
	cd build && \
	for f in *; do echo `shasum -t $$f` >> checksums.txt; done

shipit: xcompile hash
	@echo
	@echo "All Done!"