fn main() {
    let name = std::env::args().nth(1).unwrap_or_else(|| "example".to_string());
    println!("{}", greeting::greet(&name));
}
