/// Greets someone.
///
/// ```
/// assert_eq!(greeting::greet("world"), "Hello, world!");
/// ```
pub fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

#[cfg(test)]
mod tests {
    #[test]
    fn greets() {
        assert_eq!(super::greet("test"), "Hello, test!");
    }
}
