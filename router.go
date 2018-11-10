package mak

import (
	"strings"
	"unsafe"
)

// Router is a registry of all Registered routes.
type Router struct {
	Tree      *Node
	MaxParams int
	Routes    map[string]bool
}

// MakeRouter generates a new Router, ready for use
func MakeRouter() *Router {
	return &Router{
		Tree: &Node{
			handlers: map[string]Handler{},
		},
		Routes: map[string]bool{},
	}
}

// Register Registers a new route for the method and the path with the matching
// h in the r with the optional route-level (middle)wares.
func (r *Router) Register(method, path string, h Handler, wares ...Middleware) {
	if path != "/" && hasLastSlash(path) {
		path = path[:len(path)-1]
	}

	msg := ""
	if path == "" {
		msg = "route path cannot be empty"
	} else if path[0] != '/' {
		msg = "route path must start with /"
	} else if strings.Contains(path, "//") {
		msg = "route path cannot have //"
	} else if strings.Count(path, ":") > 1 {
		ps := strings.Split(path, "/")
		for _, p := range ps {
			if strings.Count(p, ":") > 1 {
				msg = "adjacent params in route path " +
					"must be separated by /"
				break
			}
		}
	} else if strings.Contains(path, "*") {
		if strings.Count(path, "*") > 1 {
			msg = "only one * is allowed in route path"
		} else if path[len(path)-1] != '*' {
			msg = "* can only appear at end of route path"
		} else if strings.Contains(path[strings.LastIndex(path, "/"):], ":") {
			msg = "adjacent param and * in route path must " + "be separated by /"
		}
	} else if rn := method + pathWithoutParamNames(path); r.Routes[rn] {
		msg = "route already exists"
	} else {
		r.Routes[rn] = true
	}

	if msg != "" {
		panic(msg)
	}

	paramNames := []string{}
	nh := func(c *Ctx) error {
		h := h
		for i := len(wares) - 1; i >= 0; i-- {
			h = wares[i](h)
		}

		return h(c)
	}

	for i, l := 0, len(path); i < l; i++ {
		if path[i] == ':' {
			j := i + 1

			r.Insert(method, path[:i], nil, static, nil)

			for ; i < l && path[i] != '/'; i++ {
			}

			paramName := path[j:i]

			for _, pn := range paramNames {
				if pn == paramName {
					panic("duplicate route param names are invalid")
				}
			}

			paramNames = append(paramNames, paramName)
			path = path[:j] + path[i:]

			if i, l = j, len(path); i == l {
				r.Insert(method, path, nh, param, paramNames)
				return
			}

			r.Insert(method, path[:i], nil, param, paramNames)
		} else if path[i] == '*' {
			r.Insert(method, path[:i], nil, static, nil)
			paramNames = append(paramNames, "*")
			r.Insert(method, path[:i+1], nh, any, paramNames)
			return
		}
	}

	r.Insert(method, path, nh, static, paramNames)
}

// Insert Inserts a new route into the `tree` of the r.
func (r *Router) Insert(
	method,
	path string,
	h Handler,
	nk NodeKind,
	paramNames []string,
) {
	if l := len(paramNames); l > r.MaxParams {
		r.MaxParams = l
	}

	cn := r.Tree // Current Node as the root of the `tree` of the r

	var (
		s  = path // Search
		nn *Node  // Next Node
		sl int    // Search length
		pl int    // Prefix length
		ll int    // LCP length
		ml int    // Max length of sl and pl
	)

	for {
		sl = len(s)
		pl = len(cn.prefix)
		ll = 0

		ml = pl
		if sl < ml {
			ml = sl
		}

		for ; ll < ml && s[ll] == cn.prefix[ll]; ll++ {
		}

		if ll == 0 {
			// At root Node
			cn.label = s[0]
			cn.prefix = s
			if h != nil {
				cn.kind = nk
				cn.handlers[method] = h
				cn.paramNames = paramNames
			}
		} else if ll < pl {
			// Split Node
			nn = &Node{
				kind:       cn.kind,
				label:      cn.prefix[ll],
				prefix:     cn.prefix[ll:],
				handlers:   cn.handlers,
				parent:     cn,
				children:   cn.children,
				paramNames: cn.paramNames,
			}

			// Reset parent Node
			cn.kind = static
			cn.label = cn.prefix[0]
			cn.prefix = cn.prefix[:ll]
			cn.children = nil
			cn.handlers = map[string]Handler{}
			cn.paramNames = nil
			cn.children = append(cn.children, nn)

			if ll == sl {
				// At parent Node
				cn.kind = nk
				cn.handlers[method] = h
				cn.paramNames = paramNames
			} else {
				// Create child Node
				nn = &Node{
					kind:       nk,
					label:      s[ll],
					prefix:     s[ll:],
					handlers:   map[string]Handler{},
					parent:     cn,
					paramNames: paramNames,
				}
				nn.handlers[method] = h
				cn.children = append(cn.children, nn)
			}
		} else if ll < sl {
			s = s[ll:]

			if nn = cn.childByLabel(s[0]); nn != nil {
				// Go deeper
				cn = nn
				continue
			}

			// Create child Node
			nn = &Node{
				kind:       nk,
				label:      s[0],
				prefix:     s,
				handlers:   map[string]Handler{},
				parent:     cn,
				paramNames: paramNames,
			}
			nn.handlers[method] = h
			cn.children = append(cn.children, nn)
		} else if h != nil {
			// Node already exists
			cn.handlers[method] = h
			cn.paramNames = paramNames
		}

		return
	}
}

// Route returns a handler Registered for the c.R.
func (r *Router) Route(c *Ctx) Handler {
	var (
		p   = c.R.URL.EscapedPath()          // Path
		s   = pathClean(p)                   // Search
		cn  = r.Tree                         // Current Node
		nn  *Node                            // Next Node
		nk  NodeKind                         // Next kind
		sn  *Node                            // Saved Node
		ss  string                           // Saved search
		sl  int                              // Search length
		pl  int                              // Prefix length
		ll  int                              // LCP length
		ml  int                              // Max length of sl and pl
		si  int                              // Start index
		pvs = make([]string, 0, r.MaxParams) // Param values
	)

	// Search order: static > param > any
	for {
		if s == "" {
			break
		}

		pl = 0
		ll = 0

		if cn.label != ':' {
			sl = len(s)
			pl = len(cn.prefix)

			ml = pl
			if sl < ml {
				ml = sl
			}

			for ; ll < ml && s[ll] == cn.prefix[ll]; ll++ {
			}
		}

		if ll != pl {
			goto Struggle
		}

		if s = s[ll:]; s == "" {
			break
		}

		// Static Node
		if nn = cn.child(s[0], static); nn != nil {
			// Save next
			if hasLastSlash(cn.prefix) {
				nk = param
				sn = cn
				ss = s
			}

			cn = nn

			continue
		}

		// Param Node
	Param:
		if nn = cn.childByKind(param); nn != nil {
			// Save next
			if hasLastSlash(cn.prefix) {
				nk = any
				sn = cn
				ss = s
			}

			cn = nn

			for si = 0; si < len(s) && s[si] != '/'; si++ {
			}

			pvs = append(pvs, unescape(s[:si]))
			s = s[si:]

			continue
		}

		// Any Node
	Any:
		if cn = cn.childByKind(any); cn != nil {
			if hasLastSlash(p) {
				si = len(p) - 1
				for ; si > 0 && p[si] == '/'; si-- {
				}

				s += p[si+1:]
			}

			if len(pvs) < len(cn.paramNames) {
				pvs = append(pvs, unescape(s))
			} else {
				pvs[len(cn.paramNames)-1] = unescape(s)
			}

			break
		}

		// Struggle for the former Node
	Struggle:
		if sn != nil {
			cn = sn
			sn = nil
			s = ss

			switch nk {
			case param:
				goto Param
			case any:
				goto Any
			}
		}

		return ErrNotFound.Envoy
	}

	if handler := cn.handlers[c.R.Method]; handler != nil {
		for i, pv := range pvs {
			pn := cn.paramNames[i]

		OuterLoop:
			for _, p := range c.params {
				if p.Name == pn {
					p.Values = append(
						[]*RequestParamValue{
							{i: pv, ctx: c},
						},
						p.Values...,
					)
					continue OuterLoop
				}
			}

			c.params = append(c.params, &RequestParam{
				Name:   pn,
				Values: []*RequestParamValue{{i: pv, ctx: c}},
			})
		}

		return handler
	} else if len(cn.handlers) != 0 {
		return ErrMethodNotAllowed.Envoy
	}

	return ErrNotFound.Envoy
}

// hasLastSlash reports whether the s has the last '/'.
func hasLastSlash(s string) bool {
	length := len(s)
	return length > 0 && s[length-1] == '/'
}

// pathWithoutParamNames returns a path from the p without the param names.
func pathWithoutParamNames(p string) string {
	for i, l := 0, len(p); i < l; i++ {
		if p[i] == ':' {
			j := i + 1

			for ; i < l && p[i] != '/'; i++ {
			}

			p = p[:j] + p[i:]
			i, l = j, len(p)

			if i == l {
				break
			}
		}
	}

	return p
}

// pathClean returns a clean path from the p.
func pathClean(p string) string {
	b := make([]byte, 0, len(p))
	for i, l := 0, len(p); i < l; {
		if p[i] == '/' {
			i++
		} else {
			b = append(b, '/')
			for ; i < l && p[i] != '/'; i++ {
				b = append(b, p[i])
			}
		}
	}

	if len(b) == 0 {
		return "/"
	}

	return *(*string)(unsafe.Pointer(&b))
}

// unescape return a normal string unescaped from the s.
func unescape(s string) string {
	// Count the %, check that they are well-formed.
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			n++
			if i+2 >= len(s) || !ishex(s[i+1]) || !ishex(s[i+2]) {
				s = s[i:]

				if len(s) > 3 {
					s = s[:3]
				}

				return ""
			}
			i += 2
		}
	}

	if n == 0 {
		return s
	}

	t := make([]byte, len(s)-2*n)
	for i, j := 0, 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			t[j] = unhex(s[i+1])<<4 | unhex(s[i+2])
			j++
			i += 2
		case '+':
			t[j] = ' '
			j++
		default:
			t[j] = s[i]
			j++
		}
	}

	return string(t)
}

// ishex reports whether the c is hex.
func ishex(c byte) bool {
	switch {
	case '0' <= c && c <= '9':
		return true
	case 'a' <= c && c <= 'f':
		return true
	case 'A' <= c && c <= 'F':
		return true
	}

	return false
}

// unhex returns the normal byte from the hex char c.
func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}

	return 0
}

// Node is the Node of the radix tree.
type Node struct {
	kind       NodeKind
	label      byte
	prefix     string
	handlers   map[string]Handler
	parent     *Node
	children   []*Node
	paramNames []string
}

// NodeKind is a kind of the `Node`.
type NodeKind uint8

// the Node kinds.
const (
	static NodeKind = iota
	param
	any
)

// child returns a child `Node` of the n by the label and the kind.
func (n *Node) child(label byte, kind NodeKind) *Node {
	for _, c := range n.children {
		if c.label == label && c.kind == kind {
			return c
		}
	}

	return nil
}

// childByLabel returns a child `Node` of the n by the l.
func (n *Node) childByLabel(l byte) *Node {
	for _, c := range n.children {
		if c.label == l {
			return c
		}
	}

	return nil
}

// childByKind returns a child `Node` of the n by the k.
func (n *Node) childByKind(k NodeKind) *Node {
	for _, c := range n.children {
		if c.kind == k {
			return c
		}
	}

	return nil
}
