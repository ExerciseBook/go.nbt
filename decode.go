package nbt

import (
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
)

func Unmarshal(compression Compression, in io.Reader, v interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok {
				err = fmt.Errorf(s)
			} else {
				err = r.(error)
			}
		}
	}()
	new(decodeState).init(compression, in).unmarshal(v)
	return
}

type decodeState struct {
	in io.Reader
}

func (d *decodeState) init(compression Compression, in io.Reader) *decodeState {
	if in == nil {
		panic(fmt.Errorf("nbt: Input stream is nil"))
	}

	switch compression {
	case Uncompressed:
		d.in = in
	case GZip:
		r, err := gzip.NewReader(in)
		if err != nil {
			panic(err)
		}
		d.in = r
	case ZLib:
		r, err := zlib.NewReader(in)
		if err != nil {
			panic(err)
		}
		d.in = r
	default:
		panic(fmt.Errorf("nbt: Unknown compression type: %d", compression))
	}

	return d
}

func (d *decodeState) unmarshal(v interface{}) {
	_, tag := d.readTag()
	d.readValue(tag, reflect.ValueOf(v).Elem())
}

func (d *decodeState) r(i interface{}) {
	err := binary.Read(d.in, binary.BigEndian, i)
	if err != nil {
		panic(err)
	}
}

// Returns the name of the tag that was read.
func (d *decodeState) readTag() (string, Tag) {
	var tag Tag
	d.r(&tag)

	if tag == TAG_End {
		return "", tag
	}

	name := d.readString()

	return name, tag
}

func (d *decodeState) readString() string {
	var length uint16
	d.r(&length)

	value := make([]byte, length)
	_, err := d.in.Read(value)
	if err != nil {
		panic(err)
	}

	return string(value)
}

func (d *decodeState) readValue(tag Tag, v reflect.Value) {
	switch v.Kind() {
	case reflect.Int, reflect.Uint:
		panic(fmt.Errorf("nbt: int and uint types are not supported for portability reasons. Try int32 or uint32."))
	}

	switch tag {
	case TAG_Byte:
		var value uint8
		d.r(&value)
		switch v.Kind() {
		case reflect.Bool:
			v.SetBool(value != 0)
		case reflect.Int8:
			v.SetInt(int64(int8(value)))
		case reflect.Uint8:
			v.SetUint(uint64(value))
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Short:
		var value uint16
		d.r(&value)
		switch v.Kind() {
		case reflect.Int16:
			v.SetInt(int64(int16(value)))
		case reflect.Uint16:
			v.SetUint(uint64(value))
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Int:
		var value uint32
		d.r(&value)
		switch v.Kind() {
		case reflect.Int32:
			v.SetInt(int64(int16(value)))
		case reflect.Uint32:
			v.SetUint(uint64(value))
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Long:
		var value uint64
		d.r(&value)
		switch v.Kind() {
		case reflect.Int64:
			v.SetInt(int64(value))
		case reflect.Uint64:
			v.SetUint(value)
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Float:
		var value float32
		d.r(&value)
		switch v.Kind() {
		case reflect.Float32:
			v.SetFloat(float64(value))
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Double:
		var value float64
		d.r(&value)
		switch v.Kind() {
		case reflect.Float64:
			v.SetFloat(value)
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Byte_Array:
		var length uint32
		d.r(&length)

		switch v.Kind() {
		case reflect.Array:
			if uint32(v.Len()) < length {
				panic(fmt.Errorf("nbt: Byte array is of length %d, but only the array given is only %d long!", length, v.Len()))
			}

			_, err := d.in.Read(v.Slice(0, int(length)).Bytes())
			if err != nil {
				panic(err)
			}

		case reflect.Slice:
			if uint32(v.Cap()) < length {
				v.Set(reflect.MakeSlice(v.Type(), int(length), int(length)))
			}

			_, err := d.in.Read(v.Slice(0, int(length)).Bytes())
			if err != nil {
				panic(err)
			}

		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_String:
		switch v.Kind() {
		case reflect.String:
			v.SetString(d.readString())
		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_List:
		var inner Tag
		d.r(&inner)
		var length uint32
		d.r(&length)

		switch v.Kind() {
		case reflect.Slice:
			if uint32(v.Cap()) < length {
				v.Set(reflect.MakeSlice(v.Type(), 0, int(length)))
			} else {
				v.Set(v.Slice(0, 0))
			}
			kind := v.Type().Elem()

			for i := uint32(0); i < length; i++ {
				value := reflect.Zero(kind)
				d.readValue(inner, value)
				v.Set(reflect.Append(v, value))
			}

		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	case TAG_Compound:
		switch v.Kind() {
		case reflect.Struct:
			fields := parseStruct(v)

			for {
				name, tag := d.readTag()
				if tag == TAG_End {
					break
				}
				if field, ok := fields[name]; ok {
					d.readValue(tag, field)
				} else {
					panic(fmt.Errorf("nbt: Unhandled %s field %s", tag, name))
				}
			}

		default:
			panic(fmt.Errorf("nbt: Tag is %s, but I don't know how to put that in a %s!", tag, v.Kind()))
		}

	// cast TAG_Int_Array:

	default:
		panic(fmt.Errorf("nbt: Unhandled tag: %s", tag))
	}
}
