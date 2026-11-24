package wasmcomponent

import "encoding/binary"

func Build(imports, instances, types []string) []byte {
	data := []byte{0, 'a', 's', 'm', 0x0d, 0, 1, 0}
	data = section(data, 7, 2, []byte{0x42, 0, 0x79})
	var imported []byte
	for _, name := range imports {
		imported = append(externName(imported, name), 0x05, 0)
	}
	data = section(data, 10, len(imports), imported)
	data = section(data, 5, 1, []byte{0x01, 0})
	var exported []byte
	for _, name := range instances {
		exported = binary.AppendUvarint(append(externName(exported, name), 0x05), uint64(len(imports)))
		exported = append(exported, 0)
	}
	for _, name := range types {
		exported = append(externName(exported, name), 0x03, 1, 0)
	}
	return section(data, 11, len(instances)+len(types), exported)
}

func externName(data []byte, name string) []byte {
	data = binary.AppendUvarint(append(data, 0), uint64(len(name)))
	return append(data, name...)
}

func section(data []byte, id byte, count int, items []byte) []byte {
	if count == 0 {
		return data
	}
	body := append(binary.AppendUvarint(nil, uint64(count)), items...)
	data = binary.AppendUvarint(append(data, id), uint64(len(body)))
	return append(data, body...)
}
