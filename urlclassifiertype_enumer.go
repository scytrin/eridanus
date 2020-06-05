// Code generated by "enumer -json -text -yaml -sql -type=URLClassifierType"; DO NOT EDIT.

//
package eridanus

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

const _URLClassifierTypeName = "FilePostListWatch"

var _URLClassifierTypeIndex = [...]uint8{0, 4, 8, 12, 17}

func (i URLClassifierType) String() string {
	if i < 0 || i >= URLClassifierType(len(_URLClassifierTypeIndex)-1) {
		return fmt.Sprintf("URLClassifierType(%d)", i)
	}
	return _URLClassifierTypeName[_URLClassifierTypeIndex[i]:_URLClassifierTypeIndex[i+1]]
}

var _URLClassifierTypeValues = []URLClassifierType{0, 1, 2, 3}

var _URLClassifierTypeNameToValueMap = map[string]URLClassifierType{
	_URLClassifierTypeName[0:4]:   0,
	_URLClassifierTypeName[4:8]:   1,
	_URLClassifierTypeName[8:12]:  2,
	_URLClassifierTypeName[12:17]: 3,
}

// URLClassifierTypeString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func URLClassifierTypeString(s string) (URLClassifierType, error) {
	if val, ok := _URLClassifierTypeNameToValueMap[s]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to URLClassifierType values", s)
}

// URLClassifierTypeValues returns all values of the enum
func URLClassifierTypeValues() []URLClassifierType {
	return _URLClassifierTypeValues
}

// IsAURLClassifierType returns "true" if the value is listed in the enum definition. "false" otherwise
func (i URLClassifierType) IsAURLClassifierType() bool {
	for _, v := range _URLClassifierTypeValues {
		if i == v {
			return true
		}
	}
	return false
}

// MarshalJSON implements the json.Marshaler interface for URLClassifierType
func (i URLClassifierType) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

// UnmarshalJSON implements the json.Unmarshaler interface for URLClassifierType
func (i *URLClassifierType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("URLClassifierType should be a string, got %s", data)
	}

	var err error
	*i, err = URLClassifierTypeString(s)
	return err
}

// MarshalText implements the encoding.TextMarshaler interface for URLClassifierType
func (i URLClassifierType) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for URLClassifierType
func (i *URLClassifierType) UnmarshalText(text []byte) error {
	var err error
	*i, err = URLClassifierTypeString(string(text))
	return err
}

// MarshalYAML implements a YAML Marshaler for URLClassifierType
func (i URLClassifierType) MarshalYAML() (interface{}, error) {
	return i.String(), nil
}

// UnmarshalYAML implements a YAML Unmarshaler for URLClassifierType
func (i *URLClassifierType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	var err error
	*i, err = URLClassifierTypeString(s)
	return err
}

func (i URLClassifierType) Value() (driver.Value, error) {
	return i.String(), nil
}

func (i *URLClassifierType) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	str, ok := value.(string)
	if !ok {
		bytes, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("value is not a byte slice")
		}

		str = string(bytes[:])
	}

	val, err := URLClassifierTypeString(str)
	if err != nil {
		return err
	}

	*i = val
	return nil
}