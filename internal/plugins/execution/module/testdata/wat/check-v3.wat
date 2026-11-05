;; Advertises check versions 1 and 3. Check 3 returns a fix and a finding in a
;; reported source; check 1 returns nothing, so the host must select check 3.
(module
  (memory (export "memory") 2)
  (data (i32.const 1024) "{\"metadata_version\":1,\"abi_version\":1,\"transport\":\"wasm-core-json\",\"id\":\"check-v3\",\"name\":\"Check 3\",\"plugin_version\":\"1.0.0\",\"engine\":{\"name\":\"check3\",\"version\":\"1.0.0\"},\"interfaces\":{\"check\":[{\"version\":1,\"export\":\"check\"},{\"version\":3,\"export\":\"check_v3\"}]},\"capabilities\":{\"script_shells\":[\"bash\"],\"check\":true,\"fixes\":true,\"reported_sources\":true,\"resources\":[\"directory\"],\"position_encoding\":\"unicode\"}}")
  (data (i32.const 8192) "{\"ok\":true,\"diagnostics\":[]}")
  (data (i32.const 12288) "{\"ok\":true,\"diagnostics\":[{\"filename\":\"s.sh\",\"code\":\"T1\",\"message\":\"fix me\",\"severity\":\"style\",\"location\":{\"row\":2,\"column\":1},\"end_location\":{\"row\":2,\"column\":4},\"fixes\":[{\"description\":\"replace\",\"replacements\":[{\"row\":2,\"end_row\":2,\"column\":1,\"end_column\":4,\"text\":\"y\"}]}]},{\"filename\":\"lib.sh\",\"code\":\"T2\",\"message\":\"sourced\",\"severity\":\"info\",\"location\":{\"row\":3,\"column\":2},\"end_location\":{\"row\":3,\"column\":5},\"source\":\"lib.sh\"}],\"errors\":[]}")
  (func (export "abi_version") (result i32) i32.const 1)
  (func (export "alloc") (param i32) (result i32) i32.const 65536)
  (func (export "dealloc") (param i32))
  (func (export "check") (param i32 i32) (result i64)
    (i64.or (i64.shl (i64.const 8192) (i64.const 32)) (i64.const 28)))
  (func (export "check_v3") (param i32 i32) (result i64)
    (i64.or (i64.shl (i64.const 12288) (i64.const 32)) (i64.const 447)))
  (func (export "validate_config") (param i32 i32) (result i64) i64.const 0)
  (func (export "metadata") (result i64)
    (i64.or (i64.shl (i64.const 1024) (i64.const 32)) (i64.const 408)))
)
