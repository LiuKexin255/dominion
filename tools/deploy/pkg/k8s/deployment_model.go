package k8s

import (
	"fmt"
	"strings"
)

type DeploymentPort struct {
	Name string
	Port int
}

type DeploymentWorkload struct {
	Name     string
	App      string
	Desc     string
	Image    string
	Replicas int32
	Ports    []DeploymentPort
}

func (w *DeploymentWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("deployment workload 为空")
	}

	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("deployment workload 缺少 name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("deployment workload 缺少 app")
	}
	if strings.TrimSpace(w.Desc) == "" {
		return fmt.Errorf("deployment workload 缺少 desc")
	}
	if strings.TrimSpace(w.Image) == "" {
		return fmt.Errorf("deployment workload 缺少 image")
	}
	if w.Replicas < 0 {
		return fmt.Errorf("deployment workload replicas 不能小于 0")
	}

	for _, port := range w.Ports {
		if strings.TrimSpace(port.Name) == "" {
			return fmt.Errorf("deployment workload 存在空端口名")
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("deployment workload 端口 %d 非法", port.Port)
		}
	}

	return nil
}
