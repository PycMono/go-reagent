package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
)

const maxExpressionLength = 256

var allowedBinaryOperators = map[string]bool{
	"+":  true,
	"-":  true,
	"*":  true,
	"/":  true,
	"%":  true,
	"**": true,
}

type calculateTool struct{}

type calculateArgs struct {
	Expression string `json:"expression"`
}

type calculateResult struct {
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
}

type arithmeticValidator struct {
	err      error
	integers map[ast.Node]*big.Int
}

func newCalculateTool() *calculateTool { return &calculateTool{} }

func (t *calculateTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         "calculate",
		Description:  "计算不含变量或函数的纯数值算术表达式，支持括号、加减乘除、整数取模和幂运算。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string", "minLength": 1, "maxLength": maxExpressionLength},
			},
			"required":             []string{"expression"},
			"additionalProperties": false,
		},
	}
}

func (t *calculateTool) Execute(ctx context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return ai.ToolOutput{}, err
	}
	arguments, err := decodeArguments[calculateArgs](raw)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	expression := strings.TrimSpace(arguments.Expression)
	if expression == "" {
		return ai.ToolOutput{}, invalidArguments("expression is required", errors.New("expression is required"))
	}
	if len(expression) > maxExpressionLength {
		return ai.ToolOutput{}, invalidArguments("expression is too long", errors.New("expression must not exceed 256 bytes"))
	}

	validator := &arithmeticValidator{integers: make(map[ast.Node]*big.Int)}
	program, err := expr.Compile(expression,
		expr.Env(map[string]any{}),
		expr.DisableAllBuiltins(),
		expr.Optimize(false),
		expr.MaxNodes(128),
		expr.Patch(validator),
		expr.AsFloat64(),
	)
	if err != nil {
		return ai.ToolOutput{}, invalidArguments("invalid calculation expression", err)
	}
	if validator.err != nil {
		return ai.ToolOutput{}, invalidArguments("unsupported calculation expression", validator.err)
	}
	if err := ctx.Err(); err != nil {
		return ai.ToolOutput{}, err
	}
	value, err := expr.Run(program, map[string]any{})
	if err != nil {
		return ai.ToolOutput{}, invalidArguments("calculation failed", err)
	}
	result, ok := value.(float64)
	if !ok || math.IsNaN(result) || math.IsInf(result, 0) {
		return ai.ToolOutput{}, invalidArguments("calculation result is not finite", errors.New("result must be a finite number"))
	}
	return jsonOutput(calculateResult{Expression: expression, Result: result})
}

func (v *arithmeticValidator) Visit(node *ast.Node) {
	if v.err != nil {
		return
	}
	switch current := (*node).(type) {
	case *ast.IntegerNode:
		v.integers[current] = big.NewInt(int64(current.Value))
	case *ast.FloatNode:
	case *ast.UnaryNode:
		if current.Operator != "+" && current.Operator != "-" {
			v.err = errors.New("unsupported unary operator")
			return
		}
		if value := v.integers[current.Node]; value != nil {
			value = new(big.Int).Set(value)
			if current.Operator == "-" {
				value.Neg(value)
			}
			v.recordInteger(current, value)
		}
	case *ast.BinaryNode:
		if !allowedBinaryOperators[current.Operator] {
			v.err = fmt.Errorf("unsupported binary operator %q", current.Operator)
			return
		}
		left, right := v.integers[current.Left], v.integers[current.Right]
		if left != nil && right != nil && current.Operator != "/" && current.Operator != "**" {
			value := new(big.Int)
			switch current.Operator {
			case "+":
				value.Add(left, right)
			case "-":
				value.Sub(left, right)
			case "*":
				value.Mul(left, right)
			case "%":
				if right.Sign() == 0 {
					v.err = errors.New("integer divide by zero")
					return
				}
				value.Rem(left, right)
			}
			v.recordInteger(current, value)
		}
	default:
		v.err = fmt.Errorf("unsupported expression node %T", current)
	}
}

func (v *arithmeticValidator) recordInteger(node ast.Node, value *big.Int) {
	limit := new(big.Int).Lsh(big.NewInt(1), uint(strconv.IntSize-1))
	minimum := new(big.Int).Neg(new(big.Int).Set(limit))
	maximum := new(big.Int).Sub(new(big.Int).Set(limit), big.NewInt(1))
	if value.Cmp(minimum) < 0 || value.Cmp(maximum) > 0 {
		v.err = errors.New("integer overflow")
		return
	}
	v.integers[node] = new(big.Int).Set(value)
}
