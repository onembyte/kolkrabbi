package cli

import (
	"context"
	"fmt"
	"strings"
)

const bashCompletion = `#!/usr/bin/env bash
_kolk_completions() {
    local cur prev words cword
    _init_completion || return

    local verbs="key model effort mode config update uninstall stats serve version doctor help completion"
    local flags="-m --model -e --effort --mode -p --print -P --permission -r --resume -s --session --output-format --debug"
    local efforts="low medium high max 1 2 3 4"
    local models="sonnet haiku opus gpt flash pro deepseek coder free auto"
    local modes="chat code agent"

    case "${prev}" in
        effort|-e|--effort)
            COMPREPLY=( $(compgen -W "${efforts}" -- "${cur}") )
            return 0
            ;;
        model|-m|--model)
            COMPREPLY=( $(compgen -W "${models}" -- "${cur}") )
            return 0
            ;;
        mode|--mode)
            COMPREPLY=( $(compgen -W "${modes}" -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            return 0
            ;;
        config)
            COMPREPLY=( $(compgen -W "get set unset show" -- "${cur}") )
            return 0
            ;;
    esac

    if [[ ${cur} == -* ]]; then
        COMPREPLY=( $(compgen -W "${flags}" -- "${cur}") )
        return 0
    fi

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${verbs}" -- "${cur}") )
        return 0
    fi
}

complete -F _kolk_completions kolk
`

const zshCompletion = `#compdef kolk

_kolk() {
    local -a verbs
    verbs=(
        'key:add an API key for any supported provider'
        'model:switch model or list available models'
        'effort:set default effort level'
        'mode:set default operational mode'
        'config:read and write saved settings'
        'update:install the latest verified release'
        'uninstall:remove kolk and everything it stored'
        'stats:local usage and rating dashboard'
        'serve:start headless event server'
        'version:print running build'
        'help:show command reference'
        'completion:generate shell completions'
    )

    _arguments -C \
        '(-m --model)'{-m,--model}'[use specific model]:model:(sonnet haiku opus gpt flash pro deepseek coder free auto)' \
        '(-e --effort)'{-e,--effort}'[select model tier]:effort:(low medium high max 1 2 3 4)' \
        '--mode[operational mode]:mode:(chat code agent)' \
        '(-p --print)'{-p,--print}'[single-shot prompt]:prompt:' \
        '(-P --permission)'{-P,--permission}'[how much may happen without asking]:tier:(ask auto-approve full-auto)' \
        '(-r --resume)'{-r,--resume}'[resume most recent session]' \
        '(-s --session)'{-s,--session}'[resume specific session]:session:' \
        '1: :->verb' \
        '*:: :->args'

    case $state in
        verb)
            _describe -t commands 'kolk command' verbs
            ;;
        args)
            case $words[1] in
                effort)
                    _values 'effort' low medium high max 1 2 3 4
                    ;;
                model)
                    _values 'model' sonnet haiku opus gpt flash pro deepseek coder free auto
                    ;;
                mode)
                    _values 'mode' chat code agent
                    ;;
                completion)
                    _values 'shell' bash zsh fish
                    ;;
                config)
                    _values 'action' get set unset show
                    ;;
            esac
            ;;
    esac
}

_kolk "$@"
`

const fishCompletion = `# kolk fish completions

set -l verbs key model effort mode config update uninstall stats serve version help completion

complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "key" -d "add an API key"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "model" -d "switch model or list catalog"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "effort" -d "set default effort level"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "mode" -d "set default operational mode"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "config" -d "read and write settings"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "update" -d "install latest release"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "uninstall" -d "remove kolk and its files"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "stats" -d "view usage dashboard"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "serve" -d "start event server"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "version" -d "print build version"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "help" -d "show command help"
complete -c kolk -f -n "not __fish_seen_subcommand_from $verbs" -a "completion" -d "generate completions"

complete -c kolk -n "__fish_seen_subcommand_from effort" -f -a "low medium high max 1 2 3 4"
complete -c kolk -n "__fish_seen_subcommand_from model" -f -a "sonnet haiku opus gpt flash pro deepseek coder free auto"
complete -c kolk -n "__fish_seen_subcommand_from mode" -f -a "chat code agent"
complete -c kolk -n "__fish_seen_subcommand_from completion" -f -a "bash zsh fish"
complete -c kolk -n "__fish_seen_subcommand_from config" -f -a "get set unset show"

complete -c kolk -s m -l model -d "use specific model" -x -a "sonnet haiku opus gpt flash pro deepseek coder free auto"
complete -c kolk -s e -l effort -d "select model tier" -x -a "low medium high max 1 2 3 4"
complete -c kolk -l mode -d "operational mode" -x -a "chat code agent"
complete -c kolk -s p -l print -d "single-shot prompt"
complete -c kolk -s P -l permission -d "how much may happen without asking" -x -a "ask auto-approve full-auto"
complete -c kolk -s r -l resume -d "resume most recent session"
`

func (a *app) runCompletion(_ context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: kolk completion <bash|zsh|fish>")
	}

	switch strings.ToLower(args[0]) {
	case "bash":
		fmt.Fprint(a.stdout, bashCompletion)
	case "zsh":
		fmt.Fprint(a.stdout, zshCompletion)
	case "fish":
		fmt.Fprint(a.stdout, fishCompletion)
	default:
		return usagef("unknown shell %q; usage: kolk completion <bash|zsh|fish>", args[0])
	}
	return nil
}
