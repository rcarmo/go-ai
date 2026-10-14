package tools

import "encoding/binary"

func imageOrientation(data []byte) int {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		for pos := uint64(12); pos+8 <= uint64(len(data)); {
			size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			start := pos + 8
			end := start + size
			if end > uint64(len(data)) {
				return 1
			}
			if string(data[pos:pos+4]) == "EXIF" {
				tiff := data[start:end]
				if len(tiff) >= 6 && string(tiff[:6]) == "Exif\x00\x00" {
					tiff = tiff[6:]
				}
				return tiffOrientation(tiff)
			}
			pos = end + (size & 1)
		}
		return 1
	}
	return jpegOrientation(data)
}
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder = binary.LittleEndian
	if string(tiff[:2]) == "MM" {
		order = binary.BigEndian
	} else if string(tiff[:2]) != "II" {
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(tiff[4:8]))
	if offset+2 > uint64(len(tiff)) {
		return 1
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	offset += 2
	for i := 0; i < count; i++ {
		if offset+12 > uint64(len(tiff)) {
			return 1
		}
		entry := tiff[offset : offset+12]
		offset += 12
		if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			value := int(order.Uint16(entry[8:10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}
