package org

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type LinkType string

const (
	TableLink  = LinkType("Table")
	FigureLink = LinkType("Figure")
	CodeLink   = LinkType("Code")
)

type InnerLink struct {
	Index int
	Name  string
	Type  LinkType
}

func (l InnerLink) Description() string {
	return fmt.Sprintf("%v %d", l.Type, l.Index)
}

func (l InnerLink) Link() string {
	return fmt.Sprintf("#%s--%s", strings.ToLower(string(l.Type)), l.Name)
}

type Comment struct{ Content string }

type Keyword struct {
	Key   string
	Value string
}

type NodeWithMeta struct {
	Node Node
	Meta Metadata
}

type Metadata struct {
	Name           string
	Caption        [][]Node
	HTMLAttributes [][]string
}

type Include struct {
	Keyword
	Resolve func() Node
}

var keywordRegexp = regexp.MustCompile(`^(\s*)#\+([^:]+):(\s+(.*)|$)`)
var commentRegexp = regexp.MustCompile(`^(\s*)#\s(.*)`)

var includeFileRegexp = regexp.MustCompile(`(?i)^"([^"]+)" (src|example|export) (\w+)$`)
var attributeRegexp = regexp.MustCompile(`(?:^|\s+)(:[-\w]+)\s+(.*)$`)

func lexKeywordOrComment(line string) (token, bool) {
	if m := keywordRegexp.FindStringSubmatch(line); m != nil {
		return token{"keyword", len(m[1]), m[2], m}, true
	} else if m := commentRegexp.FindStringSubmatch(line); m != nil {
		return token{"comment", len(m[1]), m[2], m}, true
	}
	return nilToken, false
}

func (d *Document) parseComment(i int, stop stopFn) (int, Node) {
	return 1, Comment{d.tokens[i].content}
}

func (d *Document) parseKeyword(i int, stop stopFn) (int, Node) {
	k := parseKeyword(d.tokens[i])
	switch k.Key {
	case "SETUPFILE":
		return d.loadSetupFile(k)
	case "INCLUDE":
		return d.parseInclude(k)
	case "LINK":
		if parts := strings.Split(k.Value, " "); len(parts) >= 2 {
			d.Links[parts[0]] = parts[1]
		}
		return 1, k
	case "MACRO":
		if parts := strings.Split(k.Value, " "); len(parts) >= 2 {
			d.Macros[parts[0]] = parts[1]
		}
		return 1, k
	case "NAME", "CAPTION", "ATTR_HTML":
		consumed, node := d.parseAffiliated(i, stop)
		if consumed != 0 {
			return consumed, node
		}
		fallthrough
	default:
		if _, ok := d.BufferSettings[k.Key]; ok {
			d.BufferSettings[k.Key] = strings.Join([]string{d.BufferSettings[k.Key], k.Value}, "\n")
		} else {
			d.BufferSettings[k.Key] = k.Value
		}
		return 1, k
	}
}

func (d *Document) parseAffiliated(i int, stop stopFn) (int, Node) {
	start, meta := i, Metadata{}
	for ; !stop(d, i) && d.tokens[i].kind == "keyword"; i++ {
		switch k := parseKeyword(d.tokens[i]); k.Key {
		case "NAME":
			meta.Name = k.Value
		case "CAPTION":
			meta.Caption = append(meta.Caption, d.parseInline(k.Value))
		case "ATTR_HTML":
			attributes, rest := []string{}, k.Value
			for {
				if k, m := "", attributeRegexp.FindStringSubmatch(rest); m != nil {
					k, rest = m[1], m[2]
					attributes = append(attributes, k)
					if v, m := "", attributeRegexp.FindStringSubmatchIndex(rest); m != nil {
						v, rest = rest[:m[0]], rest[m[0]:]
						attributes = append(attributes, v)
					} else {
						attributes = append(attributes, strings.TrimSpace(rest))
						break
					}
				} else {
					break
				}
			}
			meta.HTMLAttributes = append(meta.HTMLAttributes, attributes)
		default:
			return 0, nil
		}
	}
	if stop(d, i) {
		return 0, nil
	}
	consumed, node := d.parseOne(i, stop)
	if consumed == 0 || node == nil {
		return 0, nil
	}
	i += consumed

	switch n := node.(type) {
	case Table:
		d.tableCounter++
		if meta.Name == "" {
			meta.Name = fmt.Sprintf("t%d", d.tableCounter)
		}
		d.InnerLinks[meta.Name] = InnerLink{
			Index: d.tableCounter,
			Name:  meta.Name,
			Type:  TableLink,
		}
	case Block:
		if n.Name != "SRC" {
			break
		}
		d.codeCounter++
		if meta.Name == "" {
			meta.Name = fmt.Sprintf("c%d", d.codeCounter)
		}
		d.InnerLinks[meta.Name] = InnerLink{
			Index: d.codeCounter,
			Name:  meta.Name,
			Type:  CodeLink,
		}
	case RegularLink:
		if !isImageOrVideoLink(n) {
			break
		}
		d.figureCounter++
		if meta.Name == "" {
			meta.Name = fmt.Sprintf("f%d", d.figureCounter)
		}
		d.InnerLinks[meta.Name] = InnerLink{
			Index: d.figureCounter,
			Name:  meta.Name,
			Type:  FigureLink,
		}
	}
	return i - start, NodeWithMeta{node, meta}
}

func parseKeyword(t token) Keyword {
	k, v := t.matches[2], t.matches[4]
	return Keyword{strings.ToUpper(k), strings.TrimSpace(v)}
}

func (d *Document) parseInclude(k Keyword) (int, Node) {
	resolve := func() Node {
		d.Log.Printf("Bad include %#v", k)
		return k
	}
	if m := includeFileRegexp.FindStringSubmatch(k.Value); m != nil {
		path, kind, lang := m[1], m[2], m[3]
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(d.Path), path)
		}
		resolve = func() Node {
			bs, err := d.ReadFile(path)
			if err != nil {
				d.Log.Printf("Bad include %#v: %s", k, err)
				return k
			}
			return Block{strings.ToUpper(kind), []string{lang}, d.parseRawInline(string(bs)), nil}
		}
	}
	return 1, Include{k, resolve}
}

func (d *Document) loadSetupFile(k Keyword) (int, Node) {
	path := k.Value
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(d.Path), path)
	}
	if filepath.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			d.Log.Printf("Bad setup file: %#v: %s", k, err)
			return 1, k
		}
		path = filepath.Join(home, path[2:])
	}
	bs, err := d.ReadFile(path)
	if err != nil {
		d.Log.Printf("Bad setup file: %#v: %s", k, err)
		return 1, k
	}
	setupDocument := d.Configuration.Parse(bytes.NewReader(bs), path)
	if err := setupDocument.Error; err != nil {
		d.Log.Printf("Bad setup file: %#v: %s", k, err)
		return 1, k
	}
	for k, v := range setupDocument.BufferSettings {
		d.BufferSettings[k] = v
	}
	return 1, k
}

func (n Comment) String() string      { return orgWriter.WriteNodesAsString(n) }
func (n Keyword) String() string      { return orgWriter.WriteNodesAsString(n) }
func (n NodeWithMeta) String() string { return orgWriter.WriteNodesAsString(n) }
func (n Include) String() string      { return orgWriter.WriteNodesAsString(n) }
