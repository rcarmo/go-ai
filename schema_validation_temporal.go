package goai

// Date/time rules adapted from TypeBox, Copyright (c) 2017-2026 Haydn Paterson,
// MIT; see THIRD_PARTY_NOTICES.md. Keep source semantics, not time.Parse's:
// year zero, fractional leap seconds and offsets through 23:59 are accepted.
var schemaFormatMonthDays = [...]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func schemaFormatDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	year := schemaFormatDecimal(value[:4])
	month := schemaFormatDecimal(value[5:7])
	day := schemaFormatDecimal(value[8:])
	if year < 0 || month < 1 || month > 12 || day < 1 {
		return false
	}
	limit := schemaFormatMonthDays[month]
	if month == 2 && year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		limit = 29
	}
	return day <= limit
}

// All callers pass two or four bytes, so parsing cannot overflow an int.
func schemaFormatDecimal(value string) int {
	n := 0
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return -1
		}
		n = n*10 + int(value[i]-'0')
	}
	return n
}

func schemaFormatTime(value string) bool {
	if len(value) < 9 || value[2] != ':' || value[5] != ':' {
		return false
	}
	hour := schemaFormatDecimal(value[:2])
	minute := schemaFormatDecimal(value[3:5])
	second := schemaFormatDecimal(value[6:8])
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 60 {
		return false
	}
	i := 8
	if value[i] == '.' {
		i++
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	zone := value[i:]
	offset := 0
	switch {
	case zone == "Z" || zone == "z":
	case len(zone) == 6 && (zone[0] == '+' || zone[0] == '-') && zone[3] == ':':
		hours, minutes := schemaFormatDecimal(zone[1:3]), schemaFormatDecimal(zone[4:])
		if hours < 0 || hours > 23 || minutes < 0 || minutes > 59 {
			return false
		}
		offset = hours*60 + minutes
		if zone[0] == '-' {
			offset = -offset
		}
	default:
		return false // The registered time format requires a timezone.
	}
	if second < 60 {
		return true
	}
	utcMinute := hour*60 + minute - offset
	return ((utcMinute%1440)+1440)%1440 == 1439
}

func schemaFormatDateTime(value string) bool {
	return len(value) > 11 && (value[10] == 'T' || value[10] == 't') &&
		schemaFormatDate(value[:10]) && schemaFormatTime(value[11:])
}
