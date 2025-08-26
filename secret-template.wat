(module
  ;; These imports will be replaced with actual secret values
  (import "env" "API_KEY_HASH" (global $API_KEY_HASH i32))
  (import "env" "SECRET_MULTIPLIER" (global $SECRET_MULTIPLIER i32))
  
  (func $secure_compute (param i32) (result i32)
    ;; Take input, add API key hash, multiply by secret
    local.get 0
    global.get $API_KEY_HASH
    i32.add
    global.get $SECRET_MULTIPLIER  
    i32.mul)
    
  (func $verify_auth (param i32) (result i32)
    ;; Simple auth check using secret
    local.get 0
    global.get $API_KEY_HASH
    i32.eq
    if (result i32)
      i32.const 1
    else
      i32.const 0
    end)
    
  (export "secure_compute" (func $secure_compute))
  (export "verify_auth" (func $verify_auth)))