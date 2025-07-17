package main

func (cm CommandMap) getKeys() []string {
	keys := []string{}
	for key := range cm {
		keys = append(keys, key)
	}
	return keys
}

func (cm CommandMap) getValues() []Command {
	values := []Command{}
	for _, value := range cm {
		values = append(values, value)
	}
	return values
}