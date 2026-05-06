package config

import "fmt"

// ParseV3DeployConfig 解析部署配置并验证版本为 3.0。
func ParseV3DeployConfig(filePath string) (*DeployConfig, error) {
	config, err := ParseDeployConfig(filePath)
	if err != nil {
		return nil, err
	}
	if config.Version != "3.0" {
		return nil, fmt.Errorf("deploy config version is %q, expected \"3.0\"; use deploy v2 for 2.0 configs", config.Version)
	}
	return config, nil
}

// ParseV3ServiceConfig 解析服务配置并验证版本为 3.0。
func ParseV3ServiceConfig(filePath string) (*ServiceConfig, error) {
	config, err := ParseServiceConfig(filePath)
	if err != nil {
		return nil, err
	}
	if config.Version != "3.0" {
		return nil, fmt.Errorf("service config version is %q, expected \"3.0\"; upgrade config or use deploy v2", config.Version)
	}
	return config, nil
}
