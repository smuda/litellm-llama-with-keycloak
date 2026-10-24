MODULES := key-broker login-helper

.PHONY: build $(MODULES)

build: $(MODULES)

$(MODULES):
	cd $@ && go build -o bin/$@ .
