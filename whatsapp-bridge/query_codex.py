import sqlite3, sys
sys.stdout.reconfigure(encoding='utf-8', errors='replace')
conn = sqlite3.connect('store/messages.db')
cur = conn.cursor()
cur.execute("SELECT jid, name FROM chats WHERE name LIKE '%codex%' OR jid LIKE '%codex%'")
results = cur.fetchall()
print('Codex search results:', results)
if not results:
    cur.execute("SELECT jid, name FROM chats WHERE jid LIKE '%g.us%' ORDER BY name")
    print('All groups:')
    for row in cur.fetchall():
        print(repr(row))
conn.close()
