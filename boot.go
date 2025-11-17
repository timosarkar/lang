package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
"path/filepath"
	"os/exec"
)

// -------------------------------
// Lexer
// -------------------------------

type Token struct {
	Kind  string
	Value string
}

var tokenSpec = []struct {
	Name    string
	Pattern string
}{
	{"NUMBER", `\d+`},
	{"ID", `[A-Za-z_]\w*`},
	{"OP", `[+\-*/=]`},
	{"LPAREN", `\(`},
	{"RPAREN", `\)`},
	{"LBRACE", `\{`},
	{"RBRACE", `\}`},
	{"SEMI", `;`},
	{"SKIP", `[ \t\n]+`},
	{"MISMATCH", `.`},
}

var keywords = map[string]bool{
	"int":    true,
	"return": true,
}

type Lexer struct {
	code   string
	tokens []Token
}

func NewLexer(code string) *Lexer {
	return &Lexer{code: code}
}

func (l *Lexer) Tokenize() ([]Token, error) {
	regexParts := ""
	for _, spec := range tokenSpec {
		regexParts += fmt.Sprintf("(?P<%s>%s)|", spec.Name, spec.Pattern)
	}
	regexParts = regexParts[:len(regexParts)-1] // trim last |

	re := regexp.MustCompile(regexParts)
	matches := re.FindAllStringSubmatchIndex(l.code, -1)

	for _, loc := range matches {
		for i, name := range re.SubexpNames() {
			if i == 0 || loc[2*i] < 0 {
				continue
			}
			value := l.code[loc[2*i]:loc[2*i+1]]
			kind := name
			if kind == "NUMBER" {
				// keep as string, parse later
			} else if kind == "ID" && keywords[value] {
				kind = strings.ToUpper(value)
			} else if kind == "SKIP" {
				kind = ""
			} else if kind == "MISMATCH" {
				return nil, fmt.Errorf("unexpected character: %s", value)
			}
			if kind != "" {
				l.tokens = append(l.tokens, Token{Kind: kind, Value: value})
			}
		}
	}
	return l.tokens, nil
}

// -------------------------------
// AST Nodes
// -------------------------------

type Node interface{}

type Function struct {
	Name string
	Body []Node
}

type Return struct {
	Expr Node
}

type VarDecl struct {
	Name string
	Expr Node
}

type Assign struct {
	Name string
	Expr Node
}

type BinOp struct {
	Op    string
	Left  Node
	Right Node
}

// -------------------------------
// Parser
// -------------------------------

type Parser struct {
	tokens []Token
	pos    int
}

func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens}
}

func (p *Parser) peek() Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return Token{Kind: "EOF"}
}

func (p *Parser) consume(expected string) Token {
	tok := p.peek()
	if expected != "" && tok.Kind != expected {
		panic(fmt.Sprintf("expected %s, got %v", expected, tok))
	}
	p.pos++
	return tok
}

func (p *Parser) ParseFunction() *Function {
	p.consume("INT")
	name := p.consume("ID").Value
	p.consume("LPAREN")
	p.consume("RPAREN")
	p.consume("LBRACE")
	var stmts []Node
	for p.peek().Kind != "RBRACE" {
		stmts = append(stmts, p.ParseStatement())
	}
	p.consume("RBRACE")
	return &Function{Name: name, Body: stmts}
}

func (p *Parser) ParseStatement() Node {
	tok := p.peek()
	switch tok.Kind {
	case "RETURN":
		p.consume("RETURN")
		expr := p.ParseExpression()
		p.consume("SEMI")
		return &Return{Expr: expr}
	case "INT":
		p.consume("INT")
		name := p.consume("ID").Value
		var expr Node
		if p.peek().Kind == "OP" && p.peek().Value == "=" {
			p.consume("OP")
			expr = p.ParseExpression()
		}
		p.consume("SEMI")
		return &VarDecl{Name: name, Expr: expr}
	case "ID":
		name := p.consume("ID").Value
		p.consume("OP") // must be '='
		expr := p.ParseExpression()
		p.consume("SEMI")
		return &Assign{Name: name, Expr: expr}
	default:
		panic(fmt.Sprintf("unknown statement starting with %v", tok))
	}
}

func (p *Parser) ParseExpression() Node {
	var left Node
	if p.peek().Kind == "NUMBER" {
		val := p.consume("NUMBER").Value
		num, _ := strconv.Atoi(val)
		left = num
	} else {
		left = p.consume("ID").Value
	}
	for p.peek().Kind == "OP" && strings.Contains("+-*/", p.peek().Value) {
		op := p.consume("OP").Value
		var right Node
		if p.peek().Kind == "NUMBER" {
			val := p.consume("NUMBER").Value
			num, _ := strconv.Atoi(val)
			right = num
		} else {
			right = p.consume("ID").Value
		}
		left = &BinOp{Op: op, Left: left, Right: right}
	}
	return left
}

// -------------------------------
// C99 Generator
// -------------------------------

type C99Generator struct{}

func (g *C99Generator) Generate(ast Node) string {
	switch n := ast.(type) {
	case int:
		return strconv.Itoa(n)
	case string:
		return n
	case *Function:
		body := ""
		for _, stmt := range n.Body {
			body += "    " + g.Generate(stmt) + "\n"
		}
		return fmt.Sprintf("int %s(void) {\n%s}\n", n.Name, body)
	case *Return:
		return "return " + g.Generate(n.Expr) + ";"
	case *VarDecl:
		if n.Expr == nil {
			return fmt.Sprintf("int %s;", n.Name)
		}
		return fmt.Sprintf("int %s = %s;", n.Name, g.Generate(n.Expr))
	case *Assign:
		return fmt.Sprintf("%s = %s;", n.Name, g.Generate(n.Expr))
	case *BinOp:
		return fmt.Sprintf("%s %s %s", g.maybeParen(n.Left), n.Op, g.maybeParen(n.Right))
	default:
		panic(fmt.Sprintf("unknown AST node: %T", n))
	}
}

func (g *C99Generator) maybeParen(expr Node) string {
	if bin, ok := expr.(*BinOp); ok {
		return "(" + g.Generate(bin) + ")"
	}
	return g.Generate(expr)
}

func main() {
    if len(os.Args) < 2 {
        fmt.Println("Usage: go run main.go <file> [ast|lex]")
        return
    }
    inputFile := os.Args[1]
    codeBytes, _ := ioutil.ReadFile(inputFile)
    code := string(codeBytes)

    lexer := NewLexer(code)
    tokens, err := lexer.Tokenize()
    if err != nil {
        panic(err)
    }
    parser := NewParser(tokens)
    ast := parser.ParseFunction()
    gen := &C99Generator{}
    output := gen.Generate(ast)

    if len(os.Args) > 2 && os.Args[2] == "ast" {
        fmt.Printf("%#v\n", ast)
        return
    } else if len(os.Args) > 2 && os.Args[2] == "lex" {
        fmt.Printf("%#v\n", tokens)
        return
    }

    // write generated C code to a temporary .c file
    tmpFile, err := os.CreateTemp("", "out-*.c")
    if err != nil {
        panic(err)
    }
    defer os.Remove(tmpFile.Name())

    _, err = tmpFile.WriteString(output)
    if err != nil {
        panic(err)
    }
    tmpFile.Close()

    // derive output executable name from input file name
    base := filepath.Base(inputFile)           // e.g. "sample.lang"
    name := strings.TrimSuffix(base, filepath.Ext(base)) // "sample"
    exeFile := filepath.Join(".", name)        // "./sample"

    // compile with gcc into current working dir
    cmd := exec.Command("gcc", tmpFile.Name(), "-o", exeFile)
    out, err := cmd.CombinedOutput()
    if err != nil {
        fmt.Printf("%s\n", string(out))
        panic(err)
    }
}
