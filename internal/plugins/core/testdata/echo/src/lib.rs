use std::cell::RefCell;
use std::collections::BTreeMap;

const MAX_INPUT_BYTES: usize = 8 * 1024 * 1024;

const METADATA: &str = r#"{"metadata_version":1,"abi_version":1,"transport":"wasm-core-json","id":"actionlint-engine-echo","name":"Echo","plugin_version":"0.1.0","engine":{"name":"echo","version":"1.0.0"},"interfaces":{"check":[{"version":1,"export":"check"}],"validate_config":[{"version":1,"export":"validate_config"}],"metadata":[{"version":1,"export":"metadata"}]},"capabilities":{"script_shells":["python","python3"],"check":true,"validate_config":true,"config_schema":true,"max_input_bytes":8388608,"position_encoding":"unicode"},"config_schema":{}}"#;

thread_local! {
    static BUFFERS: RefCell<BTreeMap<u32, Vec<u8>>> = const { RefCell::new(BTreeMap::new()) };
}

fn store(bytes: Vec<u8>) -> u64 {
    let pointer = bytes.as_ptr() as u32;
    let length = bytes.len() as u32;
    BUFFERS.with(|buffers| buffers.borrow_mut().insert(pointer, bytes));
    (u64::from(pointer) << 32) | u64::from(length)
}

fn field<'a>(request: &'a str, name: &str) -> Option<&'a str> {
    let key = format!("\"{name}\":\"");
    let start = request.find(&key)? + key.len();
    let end = request[start..].find('"')? + start;
    Some(&request[start..end])
}

fn escape(text: &str) -> String {
    text.replace('\\', "\\\\").replace('"', "\\\"")
}

fn run_check(request: &str) -> String {
    let source = field(request, "source").unwrap_or("");
    let filename = field(request, "filename").unwrap_or("");
    if filename.is_empty() {
        return r#"{"ok":false,"errors":["filename must be nonempty and contain no NUL bytes"]}"#.to_string();
    }
    if request.contains("FAKE") {
        return r#"{"ok":false,"errors":["invalid Ruff config: unknown rule selector FAKE"]}"#.to_string();
    }
    if source.contains("trap") {
        #[cfg(target_arch = "wasm32")]
        core::arch::wasm32::unreachable();
        #[cfg(not(target_arch = "wasm32"))]
        std::process::abort();
    }
    if source.contains("hang") {
        loop {
            core::hint::black_box(());
        }
    }
    if source.contains("grow") {
        let mut blocks: Vec<Vec<u8>> = Vec::new();
        loop {
            blocks.push(vec![1u8; 1 << 20]);
            core::hint::black_box(&blocks);
        }
    }
    let mut diagnostics = Vec::new();
    if let Some(offset) = source.find("missing") {
        let column = source[..offset].chars().count() + 1;
        let end = column + "missing".chars().count();
        diagnostics.push(format!(
            r#"{{"filename":"{}","code":"F821","name":"undefined-name","severity":"error","message":"Undefined name `missing`","location":{{"row":1,"column":{column}}},"end_location":{{"row":1,"column":{end}}},"url":"https://docs.astral.sh/ruff/rules/undefined-name/"}}"#,
            escape(filename)
        ));
    }
    format!(r#"{{"ok":true,"diagnostics":[{}]}}"#, diagnostics.join(","))
}

fn run_validate(request: &str) -> String {
    if request.contains("FAKE") {
        return r#"{"ok":false,"errors":["invalid Ruff config: unknown rule selector FAKE"]}"#.to_string();
    }
    r#"{"ok":true,"errors":[]}"#.to_string()
}

fn invoke(pointer: u32, length: u32, operation: fn(&str) -> String) -> u64 {
    let response = BUFFERS.with(|buffers| {
        let buffers = buffers.borrow();
        match buffers.get(&pointer).and_then(|bytes| bytes.get(..length as usize)) {
            Some(bytes) => operation(core::str::from_utf8(bytes).unwrap_or("")),
            None => r#"{"ok":false,"errors":["invalid input allocation or length"]}"#.to_string(),
        }
    });
    store(response.into_bytes())
}

#[no_mangle]
pub extern "C" fn abi_version() -> u32 {
    1
}

#[no_mangle]
pub extern "C" fn alloc(length: u32) -> u32 {
    if length == 0 || length as usize > MAX_INPUT_BYTES {
        return 0;
    }
    (store(vec![0; length as usize]) >> 32) as u32
}

#[no_mangle]
pub extern "C" fn dealloc(pointer: u32) {
    BUFFERS.with(|buffers| buffers.borrow_mut().remove(&pointer));
}

#[no_mangle]
pub extern "C" fn check(pointer: u32, length: u32) -> u64 {
    invoke(pointer, length, run_check)
}

#[no_mangle]
pub extern "C" fn validate_config(pointer: u32, length: u32) -> u64 {
    invoke(pointer, length, run_validate)
}

#[no_mangle]
pub extern "C" fn metadata() -> u64 {
    store(METADATA.as_bytes().to_vec())
}
