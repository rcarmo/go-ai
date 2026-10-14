package tools

func imageJPEGQualities(first int) []int {
	qualities := make([]int, 0, 5)
	for _, quality := range [...]int{first, 85, 70, 55, 40} {
		duplicate := false
		for _, existing := range qualities {
			if existing == quality {
				duplicate = true
				break
			}
		}
		if !duplicate {
			qualities = append(qualities, quality)
		}
	}
	return qualities
}
