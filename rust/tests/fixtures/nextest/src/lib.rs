//! Crate with a unit test (run by nextest) and a doctest (run by `cargo test --doc`).

/// Adds two numbers.
///
/// ```
/// assert_eq!(nextest_fixture::add(2, 2), 4);
/// ```
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

/// Whether the `extra` feature is enabled.
pub fn extra() -> bool {
    cfg!(feature = "extra")
}

#[cfg(test)]
mod tests {
    #[test]
    fn add_works() {
        assert_eq!(super::add(1, 2), 3);
    }
}
