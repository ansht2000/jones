package main

func (cm commandMap) getKeys() []string {
	keys := []string{}
	for key, _ := range cm {
		keys = append(keys, key)
	}
	return keys
}

func (cm commandMap) getValues() []command {
	values := []command{}
	for _, value := range cm {
		values = append(values, value)
	}
	return values
}