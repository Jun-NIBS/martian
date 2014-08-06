%{
package main

import (
    "fmt"
    "strconv"
    "strings"
)
%}

%union{
    global    *Ast
    loc       int
    val       string
    dec       Dec
    decs      []Dec
    inparam   *InParam
    outparam  *OutParam
    params    *Params
    src       *Src
    exp       Exp
    exps      []Exp
    call      *Call
    calls     []*Call
    binding   *Binding
    bindings  *Bindings
    retstm    *ReturnStm
}

%type <val>       file_id type help type src_lang
%type <dec>       dec 
%type <decs>      dec_list
%type <inparam>   in_param
%type <outparam>  out_param
%type <params>    in_param_list out_param_list split_param_list
%type <src>       src_stm
%type <exp>       exp ref_exp
%type <exps>      exp_list
%type <call>      call_stm 
%type <calls>     call_stm_list 
%type <binding>   bind_stm
%type <bindings>  bind_stm_list
%type <retstm>    return_stm

%token SKIP INVALID 
%token SEMICOLON LBRACKET RBRACKET LPAREN RPAREN LBRACE RBRACE COMMA EQUALS
%token FILETYPE STAGE PIPELINE CALL VOLATILE SWEEP SPLIT USING SELF RETURN
%token IN OUT SRC
%token <val> ID LITSTRING NUM_FLOAT NUM_INT DOT
%token <val> PY GO SH EXEC
%token <val> INT STRING FLOAT PATH FILE BOOL TRUE FALSE NULL DEFAULT

%%
file
    : dec_list
        {{ 
            fmt.Print()
            global := Ast{[]FileLoc{}, map[string]bool{}, []*Filetype{}, []*Stage{}, []*Pipeline{}, &Callables{[]Callable{}, map[string]Callable{}}, nil}
            for _, dec := range $1 {
                switch dec := dec.(type) {
                case *Filetype:
                    global.filetypes      = append(global.filetypes, dec)
                case *Stage:
                    global.stages         = append(global.stages, dec)
                    global.callables.list = append(global.callables.list, dec)
                case *Pipeline:
                    global.pipelines      = append(global.pipelines, dec)
                    global.callables.list = append(global.callables.list, dec)
                }
            }
            mmlex.(*mmLexInfo).global = &global
        }}
    | call_stm
        {{ 
            global := Ast{[]FileLoc{}, map[string]bool{}, []*Filetype{}, []*Stage{}, []*Pipeline{}, &Callables{[]Callable{}, map[string]Callable{}},  $1} 
            mmlex.(*mmLexInfo).global = &global
        }}
    ;

dec_list
    : dec_list dec
        {{ $$ = append($1, $2) }}
    | dec
        {{ $$ = []Dec{$1} }}
    ;

dec
    : FILETYPE file_id SEMICOLON
        {{ $$ = &Filetype{Node{mmlval.loc}, $2} }}
    | STAGE ID LPAREN in_param_list out_param_list src_stm RPAREN 
        {{ $$ = &Stage{Node{mmlval.loc}, $2, $4, $5, $6, nil} }}
    | STAGE ID LPAREN in_param_list out_param_list src_stm RPAREN split_param_list
        {{ $$ = &Stage{Node{mmlval.loc}, $2, $4, $5, $6, $8} }}
    | PIPELINE ID LPAREN in_param_list out_param_list RPAREN LBRACE call_stm_list return_stm RBRACE
        {{ $$ = &Pipeline{Node{mmlval.loc}, $2, $4, $5, $8, &Callables{[]Callable{}, map[string]Callable{}}, $9} }}
    ;

file_id
    : ID DOT ID
        {{ $$ = $1 + $2 + $3 }}
    | ID
    ;

in_param_list
    : in_param_list in_param
        {{ 
            $1.list = append($1.list, $2)
            $$ = $1
        }}
    | in_param
        {{ $$ = &Params{[]Param{$1}, map[string]Param{}} }}
    ;

in_param
    : IN type ID help
        {{ $$ = &InParam{Node{mmlval.loc}, $2, $3, $4} }}
    ;

out_param_list
    : out_param_list out_param
        {{ 
            $1.list = append($1.list, $2)
            $$ = $1
        }}
    | out_param
        {{ $$ = &Params{[]Param{$1}, map[string]Param{}} }}
    ;

out_param
    : OUT type help 
        {{ $$ = &OutParam{Node{mmlval.loc}, $2, "default", $3} }}
    | OUT type ID help 
        {{ $$ = &OutParam{Node{mmlval.loc}, $2, $3, $4} }}
    ;

src_stm
    : SRC src_lang LITSTRING COMMA
        {{ $$ = &Src{Node{mmlval.loc}, $2, $3} }}
    ;

help
    : LITSTRING COMMA
        {{ $$ = $1 }}
    | COMMA
        {{ $$ = "" }}
    ;

type
    : INT
    | STRING
    | PATH
    | FLOAT
    | BOOL
    | ID
    | ID DOT ID
        {{ $$ = $1 + "." + $3 }}
    ;

src_lang
    : PY
    //| GO
    //| SH
    //| EXEC
    ;

split_param_list
    : SPLIT USING LPAREN in_param_list RPAREN
        {{ $$ = $4 }}
    ;

return_stm
    : RETURN LPAREN bind_stm_list RPAREN
        {{ $$ = &ReturnStm{Node{mmlval.loc}, $3} }}
    ;

call_stm_list
    : call_stm_list call_stm
        {{ $$ = append($1, $2) }}
    | call_stm
        {{ $$ = []*Call{$1} }}
    ;

call_stm
    : CALL ID LPAREN bind_stm_list RPAREN
        {{ $$ = &Call{Node{mmlval.loc}, false, $2, $4} }}
    | CALL VOLATILE ID LPAREN bind_stm_list RPAREN
        {{ $$ = &Call{Node{mmlval.loc}, true, $3, $5} }}
    ;

bind_stm_list
    : bind_stm_list bind_stm
        {{ 
            $1.list = append($1.list, $2)
            $$ = $1
        }}
    | bind_stm
        {{ $$ = &Bindings{[]*Binding{$1}, map[string]*Binding{} } }}
    ;

bind_stm
    : ID EQUALS exp COMMA
        {{ $$ = &Binding{Node{mmlval.loc}, $1, $3, false, ""} }}
    | ID EQUALS SWEEP LPAREN exp RPAREN COMMA
        {{ $$ = &Binding{Node{mmlval.loc}, $1, $5, true, ""} }}
    ;

exp_list
    : exp_list COMMA exp
        {{ $$ = append($1, $3) }}
    | exp
        {{ $$ = []Exp{$1} }}
    ; 

exp
    : LBRACKET exp_list RBRACKET
        {{ $$ = nil }}
    | LBRACKET RBRACKET
        {{ $$ = nil }}
    | PATH LPAREN LITSTRING RPAREN
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: $1, sval: strings.Replace($3, "\"", "", -1) } }}
    | FILE LPAREN LITSTRING RPAREN
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: $1, sval: strings.Replace($3, "\"", "", -1) } }}
    | NUM_FLOAT
        {{  // Lexer guarantees parseable float strings.
            f, _ := strconv.ParseFloat($1, 64)
            $$ = &ValExp{node:Node{mmlval.loc}, kind: "float", fval: f } 
        }}
    | NUM_INT
        {{  // Lexer guarantees parseable int strings.
            i, _ := strconv.ParseInt($1, 0, 64)
            $$ = &ValExp{node:Node{mmlval.loc}, kind: "int", ival: i } 
        }}
    | LITSTRING
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: "string", sval: strings.Replace($1, "\"", "", -1)} }}
    | TRUE
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: "bool", bval: true} }}
    | FALSE
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: "bool", bval: false} }}
    | NULL
        {{ $$ = &ValExp{node:Node{mmlval.loc}, kind: "null", null: true} }}
    | ref_exp
        {{ $$ = $1 }}
    ;

ref_exp
    : ID DOT ID
        {{ $$ = &RefExp{Node{mmlval.loc}, "call", $1, $3} }}
    | ID
        {{ $$ = &RefExp{Node{mmlval.loc}, "call", $1, "default"} }}
    | SELF DOT ID
        {{ $$ = &RefExp{Node{mmlval.loc}, "self", $3, ""} }}
    ;
%%