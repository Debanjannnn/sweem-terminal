/// Next block range `[from, to_excl)` to index, or None when nothing new is final.
/// `head - lag` is the newest block treated as final.
pub fn range(cursor: Option<u64>, start: u64, head: u64, lag: u64) -> Option<(u64, u64)> {
    let from = cursor.map_or(start, |c| c + 1);
    let to_excl = head.checked_sub(lag)? + 1;
    (from < to_excl).then_some((from, to_excl))
}

#[cfg(test)]
mod tests {
    use super::range;

    #[test]
    fn ranges() {
        assert_eq!(range(None, 100, 110, 3), Some((100, 108)));
        assert_eq!(range(Some(104), 100, 110, 3), Some((105, 108)));
        assert_eq!(range(Some(107), 100, 110, 3), None);
        assert_eq!(range(Some(200), 100, 110, 3), None);
        assert_eq!(range(None, 100, 2, 3), None);
        assert_eq!(range(None, 100, 102, 3), None);
        assert_eq!(range(None, 100, 103, 3), Some((100, 101)));
    }
}
