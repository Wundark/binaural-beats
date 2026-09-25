use std::env;
use std::path::PathBuf;

fn main() {
    // Link the Go shared library on Android. Build scripts are compiled for the
    // host, so check the target through Cargo's env vars rather than #[cfg].
    if env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("android") {
        let abi = match env::var("CARGO_CFG_TARGET_ARCH").unwrap().as_str() {
            "aarch64" => "arm64-v8a",
            "arm" => "armeabi-v7a",
            "x86_64" => "x86_64",
            "x86" => "x86",
            other => panic!("unsupported Android architecture: {other}"),
        };
        let lib_dir = PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap())
            .join("gen/android/app/src/main/jniLibs")
            .join(abi);
        println!(
            "cargo:rerun-if-changed={}",
            lib_dir.join("libbinaural.so").display()
        );
        println!("cargo:rustc-link-search=native={}", lib_dir.display());
        println!("cargo:rustc-link-lib=dylib=binaural");
    }

    tauri_build::build()
}
