-module(argparser).
-export([
    test/0,
    get_index/2,
    store_switch_arg/3,
    extract_args/3, extract_args/2,
    slice_list/3,
    store_switch/3
]).

get_index([{Ind, H} | T], Spec, All) ->
    case lists:keysearch(H, 1, Spec) of
        {_, _} ->
            get_index(T, Spec, [{Ind, H} | All]);
        false ->
            get_index(T, Spec, All)
    end;

get_index([], _, All) ->
    lists:reverse(All).

get_index(Args, Spec) ->
    get_index(lists:enumerate(Args), Spec, []).

store_switch(Switch, Value, Parsed) ->
    case lists:keyfind(Switch, 1, Parsed) of
        {_, Collected} ->
            lists:keyreplace(Switch, 1, Parsed, {Switch, [Value | Collected]});
        false ->
            lists:keystore(Switch, 1, Parsed, {Switch, [Value]})
    end.

store_switch_arg(_, [], Parsed) ->
    Parsed;

store_switch_arg(Switch, [Value | Rest], Parsed) ->
    store_switch_arg(Switch, Rest, store_switch(Switch, Value, Parsed)).

slice_list(X, Start, End) when Start >= 1; Start =< End ->
    Len = length(X),
    case End =< Len of
        true ->
            [lists:nth(I, X) || I <- lists:seq(Start, End)];
        false ->
            slice_list(X, Start, End - (End - Len))
    end.

extract_args(Args, [{Ind, Switch} | []], Parsed) ->
    Remaining = slice_list(Args, Ind, length(Args)),
    NewParsed = store_switch_arg(Switch, Remaining, Parsed),
    [{K, tl(lists:reverse(X))} || {K, X} <- NewParsed];

extract_args(Args, [{Ind, Switch} | Rest], Parsed) ->
    {NextInd, _} = hd(Rest),
    NewParsed = store_switch_arg(Switch, slice_list(Args, Ind, NextInd - 1), Parsed),
    extract_args(Args, Rest, NewParsed).

extract_args(Args, Spec) ->
    extract_args(Args, Spec, []).

get_attrib(Switch, Attrib, Spec) ->
    case lists:keyfind(Switch, 1, Spec) of
        {_, Attribs} ->
            lists:keyfind(Attrib, 1, Attribs);
        false ->
            false
    end.

wrong_nargs_error(Switch, Required, Given) ->
    error({
      wrong_nargs,
      [
       {switch, Switch},
       {required, Required},
       {given, Given}
      ]
     }).

check_nargs(Spec, []) ->
    Spec;
    
check_nargs(Spec, [{Switch, Saved} | RestParsed]) ->
    {_, N} = get_attrib(Switch, n, Spec),
    Len = length(Saved),
    if 
        N /= Len -> 
            wrong_nargs_error(Switch, N, Len);
        true ->
            check_nargs(Spec, RestParsed)
           
    end.

extract_positional(Spec, Parsed) ->
    {Switch, Saved} = lists:last(Parsed),
    SavedLen = length(Saved),
    Rest = lists:droplast(Parsed),
    check_nargs(Spec, Rest),
    {_, N} = get_attrib(Switch, n, Spec),
    if
        N == 0 andalso SavedLen == 0 ->
            NewParsed = [{Switch, []} | Rest],
            {NewParsed, Saved};
        N == 0 andalso SavedLen > 0 ->
            wrong_nargs_error(Switch, 0, SavedLen);
        N > SavedLen -> 
            wrong_nargs_error(Switch, N, SavedLen);
        true ->
            NewSaved = slice_list(Saved, 1, N),
            NewParsed = [{Switch, NewSaved} | Rest],
            NewPositional = slice_list(Saved, N + 1, SavedLen),
            {NewParsed, NewPositional}
    end.


test() ->
    Args = ["-a", "1", "2", "-b", "d", "-c", "a", "-1"],
    Spec = [{"-a", [{n, 2}]}, {"-b", [{n, 0}]}, {"-c", [{n, 1}]}],
    {Parsed, Positional} = extract_positional(Spec, extract_args(Args, get_index(Args, Spec))),
    {Parsed, Positional}.



