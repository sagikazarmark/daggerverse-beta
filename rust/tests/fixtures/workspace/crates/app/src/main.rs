fn main() {
    println!("{}", greeting::greet("app"));
}

#[cfg(test)]
mod tests {
    #[test]
    fn works() {}
}
