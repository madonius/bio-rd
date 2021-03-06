package packet

import "bytes"

func decodePathAttrFlags(buf *bytes.Buffer, pa *PathAttribute) error {
	flags := uint8(0)
	err := decode(buf, []interface{}{&flags})
	if err != nil {
		return err
	}

	pa.Optional = isOptional(flags)
	pa.Transitive = isTransitive(flags)
	pa.Partial = isPartial(flags)
	pa.ExtendedLength = isExtendedLength(flags)

	return nil
}

func isOptional(x uint8) bool {
	if x&128 == 128 {
		return true
	}
	return false
}

func isTransitive(x uint8) bool {
	if x&64 == 64 {
		return true
	}
	return false
}

func isPartial(x uint8) bool {
	if x&32 == 32 {
		return true
	}
	return false
}

func isExtendedLength(x uint8) bool {
	if x&16 == 16 {
		return true
	}
	return false
}
