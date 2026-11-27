module example.com/actionlint-plugin-insecure-download

go 1.27.0

toolchain go1.27.3

require (
	actionlint.kjanat.dev/v2/plugins/sdk/go v0.0.0
	github.com/kjanat/go-modules/cm v0.5.0
)

require go.bytecodealliance.org/cm v0.3.0 // indirect

replace actionlint.kjanat.dev/v2/plugins/sdk/go => ../../sdk/go
