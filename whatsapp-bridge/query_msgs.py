import sqlite3, sys
sys.stdout.reconfigure(encoding='utf-8', errors='replace')
conn = sqlite3.connect('store/messages.db')
cur = conn.cursor()
cur.execute("SELECT timestamp, sender, content FROM messages WHERE chat_jid='120363407895179577@g.us' ORDER BY timestamp DESC LIMIT 10")
for row in cur.fetchall():
    print(row)
conn.close()
