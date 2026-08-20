LIBSRCS := $(filter-out ../%_test.go, $(wildcard ../*.go)) ../go.mod ../go.sum
NODE_MODULES := ../node_modules
SRCS := $(wildcard src/*.ts src/*.css) index.html vite.config.ts tsconfig.json
# wasm_exec.js is part of the Go toolchain and must match the compiler that built main.wasm.
WASM_EXEC := $(shell go env GOROOT)/lib/wasm/wasm_exec.js
ASSETS := public/main.wasm public/wasm_exec.js

serve: build
	npm run preview

b build: $(NODE_MODULES) $(ASSETS) $(SRCS)
	npm run build

$(NODE_MODULES):
	npm install

dep: $(NODE_MODULES)

public/main.wasm: main.go $(LIBSRCS)
	mkdir -p public
	GOOS=js GOARCH=wasm go build -o public/main.wasm

public/wasm_exec.js: $(WASM_EXEC)
	mkdir -p public
	cp $(WASM_EXEC) public/wasm_exec.js

.testtimestamp: $(NODE_MODULES) $(ASSETS) $(SRCS)
	npm test
	touch .testtimestamp

t test: .testtimestamp

c clean:
	rm -rf ./dist ./public
	rm -f .testtimestamp

.PHONY: build dep serve test clean b t c
