package media

func EncodePCMU(sample int16) byte {
	const bias = 0x84
	sign := byte(0)
	value := int(sample)
	if value < 0 {
		sign = 0x80
		value = -value
	}
	if value > 32635 {
		value = 32635
	}
	value += bias
	exponent := 7
	for mask := 0x4000; exponent > 0 && value&mask == 0; exponent-- {
		mask >>= 1
	}
	mantissa := (value >> (exponent + 3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

func EncodePCMA(sample int16) byte {
	value := int(sample)
	mask := byte(0xd5)
	if value < 0 {
		mask = 0x55
		value = -value - 1
	}
	if value > 32767 {
		value = 32767
	}
	var encoded byte
	if value < 256 {
		encoded = byte(value >> 4)
	} else {
		exponent := 1
		for threshold := 512; exponent < 7 && value >= threshold; exponent++ {
			threshold <<= 1
		}
		encoded = byte(exponent<<4) | byte((value>>(exponent+3))&0x0f)
	}
	return encoded ^ mask
}
