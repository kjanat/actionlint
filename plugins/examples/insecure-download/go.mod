module example.com/actionlint-plugin-insecure-download

go 1.27.0

toolchain go1.27.3

require (
	actionlint.kjanat.dev/v2/plugins/sdk/go v0.0.0
	go.bytecodealliance.org/cm v0.3.0
)

replace actionlint.kjanat.dev/v2/plugins/sdk/go => ../../sdk/go
