(component
  (core module $m
    (memory (export "memory") 1)
    (func (export "realloc") (param i32 i32 i32 i32) (result i32)
      (if (result i32) (i32.eqz (local.get 3))
        (then (i32.const 0))
        (else (i32.const 1024))))
    (func (export "f") (param i32 i32))
  )
  (core instance $i (instantiate $m))
  (func (export "f") (param "s" string)
    (canon lift (core func $i "f") (memory $i "memory") (realloc (func $i "realloc")) string-encoding=utf8))
)
