#!/bin/bash

set -e -o pipefail

pkg_dir() {
	dirname "$(node -p "require.resolve('$1/package.json')")"
}

codemirror="$(pkg_dir codemirror)"
bulma="$(pkg_dir bulma)"
devicon="$(pkg_dir devicon)"
ismobilejs="$(pkg_dir ismobilejs)"
pako="$(pkg_dir pako)"

rm -rf ./lib
mkdir -p ./lib/css/fonts
mkdir -p ./lib/js

cp "${codemirror}/lib/codemirror.css" ./lib/css/
cp "${codemirror}/theme/material-darker.css" ./lib/css/
cp "${bulma}/css/bulma.min.css" ./lib/css/
cp "${devicon}/devicon.min.css" ./lib/css/
cp "${devicon}"/fonts/* ./lib/css/fonts/
cp "${codemirror}/lib/codemirror.js" ./lib/js/
cp "${codemirror}/addon/selection/active-line.js" ./lib/js/
cp "${codemirror}/mode/yaml/yaml.js" ./lib/js/
cp "${ismobilejs}/dist/isMobile.min.js" ./lib/js/
cp "${pako}/dist/pako.min.js" ./lib/js/

wasm_exec="$(go env GOROOT)/lib/wasm/wasm_exec.js"
if [ ! -f "${wasm_exec}" ]; then
	echo "${wasm_exec} does not exist"
	exit 1
fi
cat "${wasm_exec}" >./lib/js/wasm_exec.js
