(component
  (core module $implementation
    (func (export "analyze") (result i32) i32.const 0))
  (core instance $implementation (instantiate $implementation))
  (func $analyze (result u32)
    (canon lift (core func $implementation "analyze")))
  (instance $analysis (export "analyze" (func $analyze)))
  (export "actionlint:extension/analysis@0.1.0" (instance $analysis)))
