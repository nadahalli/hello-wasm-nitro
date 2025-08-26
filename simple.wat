(module
  ;; Simple square function
  (func $square (param i32) (result i32)
    local.get 0
    local.get 0
    i32.mul)
  
  ;; Simple add function
  (func $add (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)
  
  ;; Simple multiply function
  (func $multiply (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.mul)
  
  (export "square" (func $square))
  (export "add" (func $add))
  (export "multiply" (func $multiply)))