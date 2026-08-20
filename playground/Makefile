LIBSRCS := $(filter-out ../%_test.go, $(wildcard ../*.go)) ../go.mod ../go.sum
NODE_MODULES := ../node_modules

serve: build
	npm run serve

b build: $(NODE_MODULES) lib main.wasm index.js test.js

$(NODE_MODULES):
	npm install

index.js test.js: index.ts test.ts lib.d.ts tsconfig.json
	npm run build

lib: $(NODE_MODULES)
	bash ./post-install.bash

dep: $(NODE_MODULES)

main.wasm: main.go $(LIBSRCS)
	GOOS=js GOARCH=wasm go build -o main.wasm

.testtimestamp: $(NODE_MODULES) lib test.js main.wasm
	npm test
	touch .testtimestamp

t test: .testtimestamp

c clean:
	rm -f ./main.wasm ./index.js ./index.js.map .testtimestamp
	rm -rf ./lib

.PHONY: build dep serve test clean b t c
