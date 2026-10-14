package standalonecfg

import (
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

var applicationConfigType = reflect.TypeOf(Config{})

func validateConfigKeys(data []byte) error {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("read Boyan config: %w", err)
	}

	if err := validateConfigMap(document, applicationConfigType, ""); err != nil {
		return fmt.Errorf("decode Boyan config: %w", err)
	}

	return nil
}

func validateConfigValue(value any, target reflect.Type, path string) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}

	switch target.Kind() {
	case reflect.Struct:
		return validateConfigStructValue(value, target, path)
	case reflect.Map:
		return validateConfigMapValue(value, target, path)
	case reflect.Slice, reflect.Array:
		return validateConfigSliceValue(value, target, path)
	}

	return nil
}

func validateConfigStructValue(value any, target reflect.Type, path string) error {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	return validateConfigMap(mapping, target, path)
}

func validateConfigMapValue(value any, target reflect.Type, path string) error {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	for key, child := range mapping {
		if err := validateConfigValue(child, target.Elem(), joinConfigPath(path, key)); err != nil {
			return err
		}
	}

	return nil
}

func validateConfigSliceValue(value any, target reflect.Type, path string) error {
	values, ok := value.([]any)
	if !ok {
		return nil
	}

	for _, child := range values {
		if err := validateConfigValue(child, target.Elem(), path); err != nil {
			return err
		}
	}

	return nil
}

func validateConfigMap(mapping map[string]any, target reflect.Type, path string) error {
	for key, value := range mapping {
		field, ok := configField(target, key)
		if !ok {
			return fmt.Errorf("unknown key %q", joinConfigPath(path, key))
		}

		if err := validateConfigValue(value, field.Type, joinConfigPath(path, key)); err != nil {
			return err
		}
	}

	return nil
}

func configField(target reflect.Type, key string) (reflect.StructField, bool) {
	for index := range target.NumField() {
		field := target.Field(index)
		if !field.IsExported() {
			continue
		}

		name := strings.Split(field.Tag.Get("mapstructure"), ",")[0]
		if name == "" {
			name = field.Name
		}

		if name != "-" && strings.EqualFold(name, key) {
			return field, true
		}
	}

	return reflect.StructField{}, false
}

func joinConfigPath(parent, key string) string {
	if parent == "" {
		return key
	}

	return parent + "." + key
}
