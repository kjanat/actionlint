;; Returns validate_config input verbatim to exercise host response decoding.
(module
  (memory (export "memory") 1)
  (data (i32.const 1024) "\7b\22\61\62\69\5f\76\65\72\73\69\6f\6e\22\3a\31\2c\22\69\64\22\3a\22\6a\73\6f\6e\2d\70\72\6f\74\6f\63\6f\6c\22\7d")
  (func (export "abi_version") (result i32) i32.const 1)
  (func (export "alloc") (param i32) (result i32) i32.const 4096)
  (func (export "dealloc") (param i32))
  (func $echo (export "check") (export "validate_config") (param i32 i32) (result i64)
    local.get 0 i64.extend_i32_u i64.const 32 i64.shl
    local.get 1 i64.extend_i32_u i64.or)
  (func (export "metadata") (result i64) i64.const 4398046511142)
)
