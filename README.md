# Goconstruct
Goconstruct is a code generator, that generetes constructors for struct types.

For instance, for the following struct located in `struct.go` 

```
type MyStruct struct {
	hello  *int
	world  String
}
```

It creates `struct_gen.go` 
```
func NewMyStruct(hello *int,world String) *MyStruct {
	s := MyStruct{}
	s.hello = hello
	s.world = world
	return &s
}

```

## Installation
`go get github.com/domsu/goconstruct`


## Usage
```
goconstruct -type T directory
  -type string
    	comma-separated list of type names; optional

```
