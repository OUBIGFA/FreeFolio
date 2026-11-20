// Integer order keys implement the append path of rocicorp/fractional-indexing
// (CC0), as used by Quiet.md. Existing valid keys remain unchanged on save.
package whiteboard

import "strings"

const orderDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func integerLength(c byte) int {
	if c >= 'a' && c <= 'z' {
		return int(c-'a') + 2
	}
	if c >= 'A' && c <= 'Z' {
		return int('Z'-c) + 2
	}
	return 0
}
func ValidOrderKey(key string) bool {
	if key == "" {
		return false
	}
	n := integerLength(key[0])
	if n == 0 || len(key) < n {
		return false
	}
	for _, c := range []byte(key) {
		if strings.IndexByte(orderDigits, c) < 0 {
			return false
		}
	}
	return len(key) == n || key[len(key)-1] != '0'
}
func nextOrder(key string) string {
	if key == "" {
		return "a0"
	}
	n := integerLength(key[0])
	if n == 0 || len(key) < n {
		return "a0"
	}
	head := key[0]
	digits := []byte(key[1:n])
	carry := true
	for i := len(digits) - 1; i >= 0 && carry; i-- {
		at := strings.IndexByte(orderDigits, digits[i]) + 1
		if at == len(orderDigits) {
			digits[i] = '0'
		} else {
			digits[i] = orderDigits[at]
			carry = false
		}
	}
	if carry {
		if head == 'Z' {
			return "a0"
		}
		if head == 'z' {
			return key + "V"
		}
		head++
		if head > 'a' {
			digits = append(digits, '0')
		} else {
			digits = digits[:len(digits)-1]
		}
	}
	return string(head) + string(digits)
}
func normalizedOrder(es []*Element) []*Element {
	valid := true
	previous := ""
	for _, e := range es {
		if !ValidOrderKey(e.Index) || e.Index <= previous {
			valid = false
			break
		}
		previous = e.Index
	}
	if valid {
		return es
	}
	out := make([]*Element, len(es))
	previous = ""
	for i, e := range es {
		copy := *e
		copy.Index = nextOrder(previous)
		previous = copy.Index
		out[i] = &copy
	}
	return out
}
