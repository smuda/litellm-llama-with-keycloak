MODULES := key-broker login-helper

.PHONY: build e2e $(MODULES)

build: $(MODULES)

$(MODULES):
	cd $@ && go build -o bin/$@ .

e2e:
	./e2e/proxy.sh
