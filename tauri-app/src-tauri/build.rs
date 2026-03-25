fn main() {
    // Link the Go shared library on Android
    #[cfg(target_os = "android")]
    {
        println!("cargo:rustc-link-lib=dylib=binaural");
    }

    tauri_build::build()
}
