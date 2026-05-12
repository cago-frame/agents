package models

import "strings"

var registry = buildRegistry()

func buildRegistry() map[string]Info {
	m := make(map[string]Info, len(catalog)*2)
	for _, info := range catalog {
		m[strings.ToLower(info.ID)] = info
		for _, a := range info.Aliases {
			m[strings.ToLower(a)] = info
		}
	}
	return m
}

// Get 根据 id 或别名查询模型，大小写不敏感。
func Get(idOrAlias string) (Info, bool) {
	info, ok := registry[strings.ToLower(idOrAlias)]
	return info, ok
}

// MustGet 同 Get，未命中时 panic。
func MustGet(idOrAlias string) Info {
	info, ok := Get(idOrAlias)
	if !ok {
		panic("models: unknown model " + idOrAlias)
	}
	return info
}

// All 返回全部模型，顺序与 catalog 声明顺序一致。
func All() []Info {
	out := make([]Info, len(catalog))
	copy(out, catalog)
	return out
}

// ByVendor 返回指定厂商的全部模型。
func ByVendor(v Vendor) []Info {
	out := make([]Info, 0, len(catalog))
	for _, info := range catalog {
		if info.Vendor == v {
			out = append(out, info)
		}
	}
	return out
}
