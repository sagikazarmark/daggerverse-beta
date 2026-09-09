//! Crate with two independent features, to exercise feature matrices.

#[cfg(feature = "a")]
pub fn a() -> &'static str {
    "a"
}

#[cfg(feature = "b")]
pub fn b() -> &'static str {
    "b"
}

/// Features enabled at compile time.
pub fn enabled() -> Vec<&'static str> {
    let mut features = Vec::new();

    #[cfg(feature = "a")]
    features.push(a());

    #[cfg(feature = "b")]
    features.push(b());

    features
}

#[cfg(test)]
mod tests {
    #[test]
    fn enabled_matches_cfg() {
        let enabled = super::enabled();

        assert_eq!(enabled.contains(&"a"), cfg!(feature = "a"));
        assert_eq!(enabled.contains(&"b"), cfg!(feature = "b"));
    }
}
