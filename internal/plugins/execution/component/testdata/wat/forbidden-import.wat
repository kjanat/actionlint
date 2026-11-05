(component
  (import "wasi:sockets/tcp-create-socket@0.2.9" (instance))
  (instance $analyzer)
  (export "kjanat:actionlint/analyzer@0.2.0" (instance $analyzer))
)
