package myexec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lingcetech/funplugin/fungo"
	"github.com/pkg/errors"
)

var (
	logger         = fungo.Logger
	PYPI_INDEX_URL = os.Getenv("PYPI_INDEX_URL")
	PATH           = os.Getenv("PATH")
)

var python3Executable string = "python3" // system default python3

func isPython3(python string) bool {
	out, err := Command(python, "--version").Output()
	if err != nil {
		return false
	}
	if strings.HasPrefix(string(out), "Python 3") {
		return true
	}
	return false
}

// EnsurePython3Venv ensures python3 venv with specified packages
// venv should be directory path of target venv
func EnsurePython3Venv(venv string, packages ...string) (python3 string, err error) {
	// priority: specified > $HOME/.hrp/venv
	if venv == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "get user home dir failed")
		}
		venv = filepath.Join(home, ".lc", "venv")
	}
	python3, err = ensurePython3Venv(venv, packages...)
	if err != nil {
		return "", err
	}
	python3Executable = python3
	logger.Info("set python3 executable path",
		"Python3Executable", python3Executable)
	return python3, nil
}

func ExecPython3Command(cmdName string, args ...string) error {
	args = append([]string{"-m", cmdName}, args...)
	return RunCommand(python3Executable, args...)
}

func AssertPythonPackage(python3 string, pkgName, pkgVersion string) error {
	out, err := Command(
		python3, "-c", fmt.Sprintf("\"import %s; print(%s.__version__)\"", pkgName, pkgName),
	).Output()
	if err != nil {
		return fmt.Errorf("python package %s not found", pkgName)
	}

	// do not check version if pkgVersion is empty
	if pkgVersion == "" {
		logger.Info("python package is ready", "name", pkgName)
		return nil
	}

	// check package version equality
	version := strings.TrimSpace(string(out))
	if strings.TrimLeft(version, "v") != strings.TrimLeft(pkgVersion, "v") {
		return fmt.Errorf("python package %s version %s not matched, please upgrade to %s",
			pkgName, version, pkgVersion)
	}

	logger.Info("python package is ready", "name", pkgName, "version", pkgVersion)
	return nil
}

func InstallPythonPackage(python3 string, pkg string) (err error) {
	var pkgName, pkgVersion string
	if strings.Contains(pkg, "==") {
		// specify package version
		// funppy==0.5.0
		pkgInfo := strings.Split(pkg, "==")
		pkgName = pkgInfo[0]
		pkgVersion = pkgInfo[1]
	} else {
		// package version not specified, install the latest by default
		// funppy
		pkgName = pkg
	}

	// check if package installed and version matched
	err = AssertPythonPackage(python3, pkgName, pkgVersion)
	if err == nil {
		return nil
	}

	// check if pip available
	err = RunCommand(python3, "-m", "pip", "--version")
	if err != nil {
		logger.Warn("pip is not available")
		return errors.Wrap(err, "pip is not available")
	}

	logger.Info("installing python package", "pkgName",
		pkgName, "pkgVersion", pkgVersion)

	// install package
	pypiIndexURL := PYPI_INDEX_URL
	if pypiIndexURL == "" {
		pypiIndexURL = "https://pypi.org/simple" // default
	}
	err = RunCommand(python3, "-m", "pip", "install", pkg, "--upgrade",
		"--index-url", pypiIndexURL,
		"--quiet", "--disable-pip-version-check")
	if err != nil {
		return errors.Wrap(err, "pip install package failed")
	}

	return AssertPythonPackage(python3, pkgName, pkgVersion)
}

func RunShell(shellString string) (exitCode int, err error) {
	cmd := initShellExec(shellString)
	logger.Info("exec shell string", "content", cmd.String())

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return 1, errors.Wrap(err, "start running command failed")
	}

	// wait command done and get exit code
	err = cmd.Wait()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return 1, errors.Wrap(err, "get command exit code failed")
		}

		// got failed command exit code
		exitCode := exitErr.ExitCode()
		logger.Error("exec command failed", "exitCode", exitCode, "error", err)
		return exitCode, err
	}

	return 0, nil
}

func RunCommand(cmdName string, args ...string) error {
	cmd := Command(cmdName, args...)
	logger.Info("run command", "cmd", cmd.String())

	// add cmd dir path to $PATH
	if cmdDir := filepath.Dir(cmdName); cmdDir != "" {
		var path string
		if runtime.GOOS == "windows" {
			path = fmt.Sprintf("%s;%s", cmdDir, PATH)
		} else {
			path = fmt.Sprintf("%s:%s", cmdDir, PATH)
		}
		if err := os.Setenv("PATH", path); err != nil {
			logger.Error("set env $PATH failed", "error", err)
			return err
		}
	}

	_, err := RunShell(cmd.String())
	return err
}

func ExecCommandInDir(cmd *exec.Cmd, dir string) error {
	logger.Info("exec command", "cmd", cmd.String(), "dir", dir)
	cmd.Dir = dir

	// print stderr output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		logger.Error("exec command failed",
			"error", err, "stderr", stderrStr)
		if stderrStr != "" {
			err = errors.Wrap(err, stderrStr)
		}
		return err
	}

	return nil
}

func UninstallPythonPackage(python3 string, pkg string) (err error) {
	// 提取包名（忽略版本信息，卸载只需要包名）
	pkgName := pkg
	if strings.Contains(pkg, "==") {
		// 如果包含版本号，只取包名部分
		pkgInfo := strings.Split(pkg, "==")
		pkgName = pkgInfo[0]
	}

	// 检查包是否已安装（无论版本，只要安装了就需要卸载）
	// 注意：这里复用AssertPythonPackage时，若传入空版本，会检查是否存在任意版本
	err = AssertPythonPackage(python3, pkgName, "")
	if err != nil {
		// 包未安装，无需卸载
		logger.Info("python package is not installed, no need to uninstall", "pkgName", pkgName)
		return nil
	}

	// 检查pip是否可用
	err = RunCommand(python3, "-m", "pip", "--version")
	if err != nil {
		logger.Warn("pip is not available")
		return errors.Wrap(err, "pip is not available")
	}

	logger.Info("uninstalling python package", "pkgName", pkgName)

	// 执行卸载命令
	err = RunCommand(python3, "-m", "pip", "uninstall", pkgName, "-y",
		"--quiet", "--disable-pip-version-check")
	if err != nil {
		return errors.Wrap(err, "pip uninstall package failed")
	}

	// 验证卸载是否成功（检查包是否已不存在）
	err = AssertPythonPackage(python3, pkgName, "")
	if err == nil {
		// 若检查结果为未报错，说明包仍存在，卸载失败
		return errors.New("package still exists after uninstall")
	}

	logger.Info("python package uninstalled successfully", "pkgName", pkgName)
	return nil
}

func GetPythonPackage(python3 string) {
	err := RunCommand(python3, "-m", "pip", "list")
	if err != nil {
		logger.Error("failed to list python packages", "name", python3)
		return
	}
}

// InstallPip 安装pip（修复SSL证书验证错误版本）
func InstallPip(python3 string) error {
	logger.Info("检查pip是否已安装", "python3", python3)
	if err := RunCommand(python3, "-m", "pip", "--version"); err == nil {
		logger.Info("pip已安装，无需重复操作", "python3", python3)
		return nil
	}

	getPipURL := "https://bootstrap.pypa.io/get-pip.py"
	if customURL := os.Getenv("GET_PIP_URL"); customURL != "" {
		getPipURL = customURL
		logger.Info("使用自定义get-pip脚本地址", "url", getPipURL)
	}

	pythonScript := fmt.Sprintf(`
import urllib.request, sys, ssl
try:
    # 忽略SSL证书验证（适用于内部环境/无证书场景）
    ssl_context = ssl.create_default_context()
    ssl_context.check_hostname = False
    ssl_context.verify_mode = ssl.CERT_NONE
    
    # 使用带SSL上下文的请求获取脚本
    with urllib.request.urlopen("%s", context=ssl_context) as response:
        exec(response.read())
    print("pip安装成功")
except Exception as e:
    print(f"pip安装失败: {str(e)}", file=sys.stderr)
    sys.exit(1)
`, getPipURL)

	logger.Info("开始安装pip（已忽略SSL证书验证）", "url", getPipURL)
	cmd := exec.Command(python3, "-c", pythonScript)

	// 捕获输出，方便调试
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Error("pip安装失败",
			"stdout", stdout.String(),
			"stderr", stderr.String(),
			"执行脚本", pythonScript)
		return errors.Wrapf(err, "pip安装失败: %s", stderr.String())
	}

	logger.Info("验证pip安装状态")
	if err := RunCommand(python3, "-m", "pip", "--version"); err != nil {
		return errors.Wrap(err, "pip安装成功但验证失败")
	}

	logger.Info("pip安装完成", "python3", python3)
	return nil
}

// UninstallPip uninstalls pip from the specified Python3 executable
// It uses pip's own uninstall command for clean removal
func UninstallPip(python3 string) error {
	// Step 1: Check if pip is installed (skip if not present)
	logger.Info("checking if pip is installed", "python3", python3)
	err := RunCommand(python3, "-m", "pip", "--version")
	if err != nil {
		logger.Info("pip is not installed, no need to uninstall", "python3", python3)
		return nil
	}

	// Step 2: Uninstall pip (use -y to skip confirmation)
	logger.Info("uninstalling pip", "python3", python3)
	err = RunCommand(python3, "-m", "pip", "uninstall", "pip", "-y",
		"--quiet", "--disable-pip-version-check")
	if err != nil {
		return errors.Wrap(err, "failed to uninstall pip via pip command")
	}

	// Step 3: Verify pip uninstallation
	logger.Info("verifying pip uninstallation")
	err = RunCommand(python3, "-m", "pip", "--version")
	if err == nil {
		// If no error, pip is still present (uninstall failed)
		return errors.New("pip still exists after uninstallation")
	}

	logger.Info("pip uninstalled successfully", "python3", python3)
	return nil
}
