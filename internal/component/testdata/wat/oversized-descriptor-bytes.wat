(component
  (core module $m
    (memory (export "memory") 129)
    (func (export "describe") (result i32)
      (i32.store (i32.const 64) (i32.const 256))
      (i32.store (i32.const 68) (i32.const 8388609))
      (i32.const 64)))
  (core instance $i (instantiate $m))
  (type $fact (enum "source" "scripts" "mappings" "invocations" "execution"))
  (type $facts (list $fact))
  (type $descriptor (record (field "name" string) (field "requires" $facts)))
  (func $describe (result $descriptor)
    (canon lift (core func $i "describe") (memory (core memory $i "memory"))))
  (instance $analyzer
    (export "fact" (type $fact))
    (export "descriptor" (type $descriptor))
    (export "describe" (func $describe)))
  (export "kjanat:actionlint/analyzer@0.1.0" (instance $analyzer)))
