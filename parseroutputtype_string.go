// Code generated by "stringer -type=ParserOutputType"; DO NOT EDIT.

package eridanus

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Content-0]
	_ = x[Tag-1]
	_ = x[Follow-2]
	_ = x[Title-3]
	_ = x[Source-4]
	_ = x[MD5Hash-5]
}

const _ParserOutputType_name = "ContentTagFollowTitleSourceMD5Hash"

var _ParserOutputType_index = [...]uint8{0, 7, 10, 16, 21, 27, 34}

func (i ParserOutputType) String() string {
	if i < 0 || i >= ParserOutputType(len(_ParserOutputType_index)-1) {
		return "ParserOutputType(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ParserOutputType_name[_ParserOutputType_index[i]:_ParserOutputType_index[i+1]]
}