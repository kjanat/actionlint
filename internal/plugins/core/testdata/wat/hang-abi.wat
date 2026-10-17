(module
  (memory (export "memory") 1)
  (global $freed (mut i32) (i32.const 0))
  (data (i32.const 1024) "\7b\22\61\62\69\5f\76\65\72\73\69\6f\6e\22\3a\31\2c\22\69\64\22\3a\22\6c\69\66\65\63\79\63\6c\65\22\2c\22\69\6e\74\65\72\66\61\63\65\73\22\3a\7b\22\63\68\65\63\6b\22\3a\5b\7b\22\76\65\72\73\69\6f\6e\22\3a\31\2c\22\65\78\70\6f\72\74\22\3a\22\63\68\65\63\6b\22\7d\5d\2c\22\76\61\6c\69\64\61\74\65\5f\63\6f\6e\66\69\67\22\3a\5b\7b\22\76\65\72\73\69\6f\6e\22\3a\31\2c\22\65\78\70\6f\72\74\22\3a\22\76\61\6c\69\64\61\74\65\5f\63\6f\6e\66\69\67\22\7d\5d\7d\7d")
  (data (i32.const 2048) "\7b\22\6f\6b\22\3a\74\72\75\65\2c\22\64\69\61\67\6e\6f\73\74\69\63\73\22\3a\5b\5d\7d")
  
  (func (export "abi_version") (result i32) (loop $spin (br $spin)) i32.const 1)
  (func (export "alloc") (param i32) (result i32) i32.const 4096)
  (func (export "dealloc") (param i32) )
  (func (export "check") (param i32 i32) (result i64) i64.const 8796093022236)
  (func (export "validate_config") (param i32 i32) (result i64) i64.const 8796093022236)
  (func (export "metadata") (result i64) i64.const 4398046511255)
)
