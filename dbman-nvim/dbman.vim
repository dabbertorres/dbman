if exists('g:loaded_dbman')
    finish
endif
let g:loaded_dbman = 1

function! s:Requiredbman(host) abort
    return jobstart(['dbman-nvim'], {'rpc': v:true})
endfunction

call remote#host#Register('dbman-nvim', 'x', function('s:Requiredbman'))

call remote#host#RegisterPlugin('dbman-nvim', '0', [
\ {'type': 'command', 'name': 'DBConnect', 'sync': 1, 'opts': {'complete': 'custom,DBConnectionsF', 'nargs': '1'}},
\ {'type': 'command', 'name': 'DBConnections', 'sync': 1, 'opts': {'nargs': '0'}},
\ {'type': 'command', 'name': 'DBRun', 'sync': 1, 'opts': {'addr': 'lines', 'bar': '', 'nargs': '?', 'range': '%'}},
\ {'type': 'command', 'name': 'DBSchemas', 'sync': 1, 'opts': {'nargs': '0'}},
\ {'type': 'command', 'name': 'DBTables', 'sync': 1, 'opts': {'nargs': '*'}},
\ {'type': 'function', 'name': 'DBConnectionsF', 'sync': 1, 'opts': {}},
\ ])
