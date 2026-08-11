package badger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

func newCanonicalLogicalImportScanner(source io.Reader) canonicalLogicalImportScanner {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), maxCanonicalLogicalLineBytes)
	return canonicalLogicalImportScanner{scanner: scanner}
}

type canonicalLogicalImportScanner struct {
	scanner    *bufio.Scanner
	lineNumber int
}

func (s *canonicalLogicalImportScanner) readJSON(target any) error {
	line, err := s.readLine()
	if err != nil {
		return err
	}
	return s.unmarshal(line, target)
}

func (s *canonicalLogicalImportScanner) readLine() ([]byte, error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	}
	s.lineNumber++
	return append([]byte(nil), s.scanner.Bytes()...), nil
}

func (s *canonicalLogicalImportScanner) unmarshal(line []byte, target any) error {
	if err := json.Unmarshal(line, target); err != nil {
		return fmt.Errorf("decode canonical logical export line %d: %w", s.lineNumber, err)
	}
	return nil
}

func (s *canonicalLogicalImportScanner) hasTrailingRecord() bool {
	return s.scanner.Scan()
}

func (s *canonicalLogicalImportScanner) err() error {
	return s.scanner.Err()
}
