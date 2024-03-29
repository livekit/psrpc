// Copyright 2018 Twitch Interactive, Inc.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may not
// use this file except in compliance with the License. A copy of the License is
// located at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// or in the "license" file accompanying this file. This file is distributed on
// an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package stringutils

import (
	"strings"
	"unicode"
)

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// CamelCase converts a string from snake_case to CamelCased.
//
// If there is an interior underscore followed by a lower case letter, drop the
// underscore and convert the letter to upper case. There is a remote
// possibility of this rewrite causing a name collision, but it's so remote
// we're prepared to pretend it's nonexistent - since the C++ generator
// lowercases names, it's extremely unlikely to have two fields with different
// capitalizations. In short, _my_field_name_2 becomes XMyFieldName_2.
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	t := make([]byte, 0, 32)
	i := 0
	if s[0] == '_' {
		// Need a capital letter; drop the '_'.
		t = append(t, 'X')
		i++
	}
	// Invariant: if the next letter is lower case, it must be converted
	// to upper case.
	//
	// That is, we process a word at a time, where words are marked by _ or upper
	// case letter. Digits are treated as words.
	for ; i < len(s); i++ {
		c := s[i]
		if c == '_' && i+1 < len(s) && isASCIILower(s[i+1]) {
			continue // Skip the underscore in s.
		}
		if isASCIIDigit(c) {
			t = append(t, c)
			continue
		}
		// Assume we have a letter now - if not, it's a bogus identifier. The next
		// word is a sequence of characters that must start upper case.
		if isASCIILower(c) {
			c ^= ' ' // Make it a capital letter.
		}
		t = append(t, c) // Guaranteed not lower case.
		// Accept lower case sequence that follows.
		for i+1 < len(s) && isASCIILower(s[i+1]) {
			i++
			t = append(t, s[i])
		}
	}
	return string(t)
}

// LowerCamelCase converts a snake_case string to camelCase
func LowerCamelCase(s string) string {
	t := []byte(CamelCase(s))
	t[0] ^= ' '
	return string(t)
}

// AlphaDigitize replaces non-letter, non-digit, non-underscore characters with
// underscore.
func AlphaDigitize(r rune) rune {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return r
	}
	return '_'
}

// CleanIdentifier makes sure s is a valid 'identifier' string: it contains only
// letters, numbers, and underscore.
func CleanIdentifier(s string) string {
	return strings.Map(AlphaDigitize, s)
}

// BaseName the last path element of a slash-delimited name, with the last
// dotted suffix removed.
func BaseName(name string) string {
	// First, find the last element
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// Now drop the suffix
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[0:i]
	}
	return name
}
