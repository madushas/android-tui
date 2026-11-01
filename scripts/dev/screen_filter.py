import sys,re
pat=re.compile(sys.argv[2]) if len(sys.argv)>2 else None
x0=int(sys.argv[1]) if len(sys.argv)>1 else 42
b=open('/tmp/out.txt','rb').read().decode('utf-8','replace')
for blk in b.split('──── ')[1:]:
    ls=blk.split('\n'); print('════',ls[0][:26])
    for l in ls[1:]:
        t=l[x0:].rstrip(' │')
        if t.strip(' │─┐┘└┌') and (not pat or pat.search(t)): print(t[:120])
print(b[-30:].strip())
