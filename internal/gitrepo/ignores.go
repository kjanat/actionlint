package gitrepo

import (
	"path"
	"path/filepath"
	"strings"
)

type Ignores struct {
	repo  *Repository
	nodes map[string]*ignoreNode
}

type ignoreNode struct {
	parent   *ignoreNode
	dir      string
	patterns *patternList
	ignored  bool
}

func (r *Repository) Ignores() (*Ignores, error) {
	root := &ignoreNode{patterns: newPatternList(r.ignoreCase)}
	for _, file := range []string{r.excludes, filepath.Join(r.CommonDir, "info", "exclude"), filepath.Join(r.WorkTree, ".gitignore")} {
		if file == "" {
			continue
		}
		data, err := readOptional(file)
		if err != nil {
			return nil, err
		}
		root.patterns.addBuffer(data)
	}
	return &Ignores{repo: r, nodes: map[string]*ignoreNode{"": root}}, nil
}

func (i *Ignores) Ignored(relative string, isDir bool) (bool, error) {
	node, err := i.node(parentDir(relative))
	if err != nil {
		return false, err
	}
	if node.ignored {
		return true, nil
	}
	return node.matched([]byte(relative), isDir) == patternPositive, nil
}

func (i *Ignores) node(dir string) (*ignoreNode, error) {
	if node, ok := i.nodes[dir]; ok {
		return node, nil
	}
	parent, err := i.node(parentDir(dir))
	if err != nil {
		return nil, err
	}
	node := &ignoreNode{parent: parent, dir: dir}
	node.ignored = parent.ignored || parent.matched([]byte(dir), true) == patternPositive
	if !node.ignored {
		data, err := readOptional(filepath.Join(i.repo.WorkTree, filepath.FromSlash(dir), ".gitignore"))
		if err != nil {
			return nil, err
		}
		if data != nil {
			node.patterns = newPatternList(i.repo.ignoreCase)
			node.patterns.addBuffer(data)
		}
	}
	i.nodes[dir] = node
	return node, nil
}

func (n *ignoreNode) matched(relative []byte, isDir bool) patternMatch {
	for node := n; node != nil; node = node.parent {
		if node.patterns == nil {
			continue
		}
		scoped := relative
		if node.dir != "" {
			rest, ok := strings.CutPrefix(string(relative), node.dir+"/")
			if !ok {
				continue
			}
			scoped = []byte(rest)
		}
		if m := node.patterns.matched(scoped, isDir); m != patternNone {
			return m
		}
	}
	return patternNone
}

func parentDir(relative string) string {
	dir := path.Dir(relative)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
