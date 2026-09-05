//! Links against Security.framework directly: succeeds only when a macOS SDK is available.

use std::ffi::c_void;
use std::ptr;

#[link(name = "Security", kind = "framework")]
extern "C" {
    fn SecRandomCopyBytes(rnd: *const c_void, count: usize, bytes: *mut u8) -> i32;
}

fn main() {
    let mut buf = [0u8; 16];

    // kSecRandomDefault is documented to be NULL.
    let status = unsafe { SecRandomCopyBytes(ptr::null(), buf.len(), buf.as_mut_ptr()) };

    println!("{status} {buf:?}");
}
