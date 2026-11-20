package build

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildVersionInjection 在临时目录构建最小 EXE，校验 JSON、资源属性及运行时版本，不改项目版本文件。
func TestBuildVersionInjection(t *testing.T) {
	for _, name := range []string{"cmd.exe", "powershell.exe", "go", "goversioninfo"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("构建版本集成测试需要 %s", name)
		}
	}
	root := repositoryRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "build.bat"))
	if err != nil {
		t.Fatal(err)
	}
	fullScript, err := os.ReadFile(filepath.Join(root, "scripts", "build.bat"))
	if err != nil {
		t.Fatal(err)
	}
	psScript, err := os.ReadFile(filepath.Join(root, "scripts", "stamp_version.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"build.bat": script, "scripts/build.bat": fullScript} {
		if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) || bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), nil), []byte("\n")) {
			t.Fatalf("%s 必须为无 BOM、CRLF 批处理", name)
		}
		for _, b := range data {
			if b > 127 {
				t.Fatalf("%s 必须保持 ASCII", name)
			}
		}
	}
	fixtureFiles := map[string][]byte{}
	for _, dir := range []string{"internal/assets", "internal/webui"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			fixtureFiles[rel] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(root, "build", "windows", "versioninfo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var template map[string]any
	if err := json.Unmarshal(metadata, &template); err != nil {
		t.Fatal(err)
	}
	template["IconPath"] = "internal/assets/favicon-128x128.ico"
	template["StringFileInfo"].(map[string]any)["LegalCopyright"] = "Copyright © 2001 ErWa"
	metadata, err = json.MarshalIndent(template, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"CMD", "PowerShell51"} {
		t.Run(shell, func(t *testing.T) {
			for _, tc := range []struct {
				name, version, tag, label string
				parts                     [4]int
				invalid                   bool
			}{
				{name: "环境变量优先于标签", version: "v2.0.1", tag: "v1.0.1", label: "2.0.1.0", parts: [4]int{2, 0, 1, 0}},
				{name: "首尾空格自动修剪", version: "  v2.0.1 \t", tag: "v1.0.1", label: "2.0.1.0", parts: [4]int{2, 0, 1, 0}},
				{name: "包裹引号自动修剪", version: `"v2.0.1"`, label: "2.0.1.0", parts: [4]int{2, 0, 1, 0}},
				{name: "四段版本", version: "2.0.1.7", label: "2.0.1.7", parts: [4]int{2, 0, 1, 7}},
				{name: "预发布版本", version: "v2.0.1-beta.1+build.7", label: "2.0.1-beta.1+build.7", parts: [4]int{2, 0, 1, 0}},
				{name: "最近标签", tag: "v3.4.5", label: "3.4.5.0", parts: [4]int{3, 4, 5, 0}},
				{name: "无标签回退", label: "dev"},
				{name: "数字上限", version: "v65535.0.0", label: "65535.0.0.0", parts: [4]int{65535, 0, 0, 0}},
				{name: "非法格式", version: "v2.0.bad", invalid: true},
				{name: "版本溢出", version: "v65536.0.0", invalid: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					base := t.TempDir()
					dir := filepath.Join(base, "项目 构建")
					cwd := filepath.Join(base, "外部 工作目录")
					if err := os.MkdirAll(cwd, 0o700); err != nil {
						t.Fatal(err)
					}
					write := func(name string, data []byte) {
						t.Helper()
						if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(name, data, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					for name, data := range fixtureFiles {
						write(filepath.Join(dir, name), data)
					}
					write(filepath.Join(cwd, "launch.cmd"), []byte("@echo off\r\ncall \"%CANYOU_TEST_BUILD%\"\r\nexit /b %errorlevel%\r\n"))
					write(filepath.Join(cwd, "launch.ps1"), []byte("if ($PSVersionTable.PSVersion.Major -ne 5) { exit 99 }\r\n& $env:CANYOU_TEST_BUILD\r\nexit $LASTEXITCODE\r\n"))
					gitStub := "@echo off\r\necho called>git-called.txt\r\nexit /b 1\r\n"
					if tc.tag != "" {
						gitStub = "@echo off\r\necho called>git-called.txt\r\necho " + tc.tag + "\r\nexit /b 0\r\n"
					}
					for name, content := range map[string][]byte{
						"git.cmd":                        []byte(gitStub),
						"build.bat":                      script,
						"scripts/build.bat":              fullScript,
						"scripts/stamp_version.ps1":      psScript,
						"build/windows/versioninfo.json": metadata,
						"go.mod":                         []byte("module buildfixture\n\ngo 1.22\n"),
						"cmd/canyou/main.go": []byte(`package main
import ("bytes"; "fmt"; "html"; "strings"; "buildfixture/internal/assets"; "buildfixture/internal/webui")
var Version = "dev"
func main() {
 var page bytes.Buffer
 if len(assets.Icon()) == 0 { panic("icon") }
 if err := webui.RenderFiles(&page, webui.FilesData{CanUpload:true,CanChat:true,Version:Version}); err != nil { panic(err) }
 if !bytes.Contains(page.Bytes(), []byte("chatToggle")) { panic("template") }
 if !strings.Contains(html.UnescapeString(page.String()), "在同一网络，轻松分享 · "+Version+"</div>") { panic("files version") }
 page.Reset()
 if err := webui.RenderGate(&page, "/", Version); err != nil { panic(err) }
 if !bytes.Contains(page.Bytes(), []byte("Canyou 共享 · "+Version+"</p>")) { panic("gate version") }
 fmt.Print(Version)
}
`),
					} {
						write(filepath.Join(dir, name), content)
					}
					run := func(name string, args ...string) ([]byte, error) {
						t.Helper()
						ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
						defer cancel()
						command := exec.CommandContext(ctx, name, args...)
						command.Dir = cwd
						command.Env = append(os.Environ(), "VER="+tc.version, "VERFILE="+filepath.Join(base, "normalized-ver.txt"), "CANYOU_TEST_BUILD="+filepath.Join(dir, "build.bat"), "GOOS=windows", "GOARCH=amd64", "GOWORK=off", "GOFLAGS=")
						return command.CombinedOutput()
					}
					mustRun := func(name string, args ...string) []byte {
						t.Helper()
						output, err := run(name, args...)
						if err != nil {
							t.Fatalf("%s 执行失败: %v\n%s", name, err, output)
						}
						return output
					}
					startYear := time.Now().Year()
					var output []byte
					var buildErr error
					if shell == "CMD" {
						output, buildErr = run("cmd.exe", "/d", "/c", "launch.cmd")
					} else {
						output, buildErr = run("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(cwd, "launch.ps1"))
					}
					endYear := time.Now().Year()
					_, gitCalledErr := os.Stat(filepath.Join(dir, "git-called.txt"))
					if (tc.version == "" && gitCalledErr != nil) || (tc.version != "" && !os.IsNotExist(gitCalledErr)) {
						t.Fatalf("VER 与 Git 标签优先级错误: %v", gitCalledErr)
					}
					stamped, err := os.ReadFile(filepath.Join(dir, "build", "windows", "versioninfo.json"))
					if err != nil {
						t.Fatal(err)
					}
					if tc.invalid {
						if buildErr == nil || !bytes.Equal(stamped, metadata) {
							t.Fatalf("非法版本应中止构建且不改写元数据: %v\n%s", buildErr, output)
						}
						if _, err := os.Stat(filepath.Join(dir, "cmd", "canyou", "resource.syso")); !os.IsNotExist(err) {
							t.Fatalf("非法版本不应生成资源: %v", err)
						}
						return
					}
					if buildErr != nil {
						t.Fatalf("版本构建失败: %v\n%s", buildErr, output)
					}
					var info struct {
						FixedFileInfo struct {
							FileVersion, ProductVersion map[string]int
						}
						StringFileInfo map[string]string
					}
					if err := json.Unmarshal(stamped, &info); err != nil {
						t.Fatalf("版本 JSON 无法解析（应为无 BOM 的 UTF-8）: %v", err)
					}
					for key, parts := range map[string]map[string]int{
						"FileVersion": info.FixedFileInfo.FileVersion, "ProductVersion": info.FixedFileInfo.ProductVersion,
					} {
						if info.StringFileInfo[key] != tc.label {
							t.Fatalf("%s 文字版本错误: %q", key, info.StringFileInfo[key])
						}
						for i, part := range []string{"Major", "Minor", "Patch", "Build"} {
							if parts[part] != tc.parts[i] {
								t.Fatalf("%s.%s 数字版本错误: %v", key, part, parts)
							}
						}
					}
					copyright := info.StringFileInfo["LegalCopyright"]
					if copyright != fmt.Sprintf("Copyright © %d ErWa", startYear) && copyright != fmt.Sprintf("Copyright © %d ErWa", endYear) {
						t.Fatalf("版权年份未取构建时的当前年份，或版权文字被改动: %q", copyright)
					}
					for key, value := range template["StringFileInfo"].(map[string]any) {
						if key != "FileVersion" && key != "ProductVersion" && key != "LegalCopyright" && info.StringFileInfo[key] != value {
							t.Fatalf("非构建字段被改动: %s", key)
						}
					}
					version := strings.Trim(tc.version, ` 	"`)
					if version == "" {
						version = tc.tag
					}
					if version == "" {
						version = "dev"
					}
					executable := "Canyou-" + version + ".exe"
					built := filepath.Join(dir, "dist", "bin", executable)
					if _, err := os.Stat(filepath.Join(dir, "dist", executable)); !os.IsNotExist(err) {
						t.Fatalf("EXE 应仅输出到 dist/bin，旧位置不应生成产物: %v", err)
					}
					assertEmbeddedIcon(t, built, fixtureFiles[filepath.FromSlash("internal/assets/favicon-128x128.ico")])
					isolated := filepath.Join(cwd, executable)
					data, err := os.ReadFile(built)
					if err != nil {
						t.Fatal(err)
					}
					write(isolated, data)
					runtimeVersion := mustRun(isolated)
					if strings.TrimSpace(string(runtimeVersion)) != version {
						t.Fatalf("程序运行时版本未正确注入: %s", runtimeVersion)
					}
					properties := mustRun("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding = New-Object Text.UTF8Encoding($false); (Get-Item -LiteralPath './"+executable+"').VersionInfo | Select-Object LegalCopyright,FileVersion,ProductVersion,FileMajorPart,FileMinorPart,FileBuildPart,FilePrivatePart,ProductMajorPart,ProductMinorPart,ProductBuildPart,ProductPrivatePart | ConvertTo-Json -Compress")
					var resource map[string]any
					if err := json.Unmarshal(properties, &resource); err != nil {
						t.Fatalf("无法读取 EXE 版本属性: %v\n%s", err, properties)
					}
					if resource["LegalCopyright"] != copyright {
						t.Fatalf("EXE 版权未使用新年份: %v", resource["LegalCopyright"])
					}
					for _, kind := range []string{"File", "Product"} {
						if resource[kind+"Version"] != tc.label {
							t.Fatalf("EXE %sVersion 未使用新版本: %v", kind, resource)
						}
						for i, part := range []string{"Major", "Minor", "Build", "Private"} {
							if resource[kind+part+"Part"] != float64(tc.parts[i]) {
								t.Fatalf("EXE 数字版本不匹配: %v", resource)
							}
						}
					}
				})
			}
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("无法定位仓库根目录")
		}
		dir = parent
	}
}

// assertEmbeddedIcon 检查 PE 资源中的原始图标图像，不把系统默认图标误认成应用图标。
func assertEmbeddedIcon(t *testing.T, executable string, icon []byte) {
	t.Helper()
	file, err := pe.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	section := file.Section(".rsrc")
	if section == nil {
		t.Fatal("EXE 缺少 Windows 资源")
	}
	data, err := section.Data()
	if err != nil {
		t.Fatal(err)
	}
	if len(icon) < 22 {
		t.Fatal("图标夹具无效")
	}
	count := int(binary.LittleEndian.Uint16(icon[4:6]))
	for i := 0; i < count; i++ {
		entry := 6 + i*16
		if entry+16 > len(icon) {
			t.Fatal("图标目录无效")
		}
		length := int(binary.LittleEndian.Uint32(icon[entry+8:]))
		offset := int(binary.LittleEndian.Uint32(icon[entry+12:]))
		if length == 0 || offset+length > len(icon) || !bytes.Contains(data, icon[offset:offset+length]) {
			t.Fatal("EXE 未包含应用图标图像")
		}
	}
}
