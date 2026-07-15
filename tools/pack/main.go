// =============================================================================
// Mattermost 플러그인 배포용 tar.gz packer.
//
// 사용:
//   go run tools/pack/main.go <source-dir> <output.tar.gz>
//
// 예시:
//   go run tools/pack/main.go dist/ai_service_reporter dist/ai_service_reporter.tar.gz
//
// 특징:
//   · 디렉토리 = 0755, 일반 파일 = 0644
//   · plugin-linux-* / plugin-darwin-* / plugin-windows-* = 0755 (executable bit 보장)
//   · Windows 파일시스템에서도 Unix 권한이 정확히 박힘
//   · 외부 의존성 없음 (Go stdlib 만 사용)
//
// 만든 이유:
//   Windows 의 bsdtar (tar.exe) 가 cross-compile 된 ELF 바이너리에 exec 비트를
//   안 붙여서, Mattermost 가 extract 시 plugin-linux-amd64 를 실행하지 못함.
// =============================================================================

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: pack <source-dir> <output.tar.gz>")
		os.Exit(2)
	}
	srcDir := os.Args[1]
	outPath := os.Args[2]

	info, err := os.Stat(srcDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "source-dir 접근 실패: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "source-dir 는 디렉토리여야 합니다: %s\n", srcDir)
		os.Exit(1)
	}

	out, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "출력 파일 생성 실패: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// rootParent 기준 상대경로로 entry name 생성.
	// 예: srcDir = D:\proj\dist\ai_service_reporter → entry 가 "ai_service_reporter/..." 로 시작.
	rootParent := filepath.Dir(srcDir)

	count := 0
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(rootParent, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		mode := determineMode(rel, info)
		header := &tar.Header{
			Name:    rel,
			Mode:    int64(mode),
			ModTime: info.ModTime(),
		}

		if info.IsDir() {
			header.Typeflag = tar.TypeDir
			if !strings.HasSuffix(header.Name, "/") {
				header.Name += "/"
			}
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = info.Size()
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			f, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer f.Close()
			if _, copyErr := io.Copy(tw, f); copyErr != nil {
				return copyErr
			}
		}
		count++
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "tar.gz 생성 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("OK -> %s (%d entries)\n", outPath, count)
}

// determineMode 는 경로/타입을 보고 Unix 권한 모드를 결정합니다.
func determineMode(rel string, info os.FileInfo) os.FileMode {
	if info.IsDir() {
		return 0755
	}
	base := filepath.Base(rel)
	// Mattermost 플러그인 실행 바이너리는 모두 0755
	if strings.HasPrefix(base, "plugin-linux") ||
		strings.HasPrefix(base, "plugin-darwin") {
		return 0755
	}
	// .exe 는 Windows 라 권한 비트 의미 없지만 일관성 위해 0755
	if strings.HasPrefix(base, "plugin-windows") {
		return 0755
	}
	return 0644
}
