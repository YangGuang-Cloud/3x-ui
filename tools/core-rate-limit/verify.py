#!/usr/bin/env python3
"""Real VLESS/TLS Vision + WebSocket bandwidth test on loopback only."""
import argparse, hashlib, ssl, concurrent.futures, http.server, json, os, pathlib, socket, struct, subprocess, tempfile, threading, time, uuid

BODY=b'x'*(512*1024)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def receive(s,n):
 out=b''
 while len(out)<n:
  part=s.recv(n-len(out))
  if not part:raise RuntimeError('unexpected EOF')
  out+=part
 return out
def proxy_connect(proxy,target):
 s=socket.create_connection(('127.0.0.1',proxy),timeout=30)
 s.sendall(b'\x05\x01\x00');assert receive(s,2)==b'\x05\x00'
 s.sendall(b'\x05\x01\x00\x01'+socket.inet_aton('127.0.0.1')+struct.pack('!H',target))
 answer=receive(s,4);assert answer[1]==0
 if answer[3]==1:receive(s,6)
 elif answer[3]==4:receive(s,18)
 else:receive(s,receive(s,1)[0]+2)
 return s
def verify_udp(proxy):
 echo=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);echo.bind(('127.0.0.1',0));echo.settimeout(.2)
 stopped=threading.Event()
 def run():
  while not stopped.is_set():
   try:
    data,address=echo.recvfrom(65535);echo.sendto(data,address)
   except socket.timeout:pass
 thread=threading.Thread(target=run,daemon=True);thread.start()
 control=socket.create_connection(('127.0.0.1',proxy),timeout=10)
 try:
  control.sendall(b'\x05\x01\x00');assert receive(control,2)==b'\x05\x00'
  control.sendall(b'\x05\x03\x00\x01'+b'\x00'*6)
  header=receive(control,4);assert header[1]==0 and header[3]==1
  address=receive(control,6);relay=(socket.inet_ntoa(address[:4]),struct.unpack('!H',address[4:])[0])
  if relay[0]=='0.0.0.0':relay=('127.0.0.1',relay[1])
  with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as udp:
   udp.settimeout(5)
   for i in range(8):
    payload=bytes([i])*1200
    packet=b'\x00\x00\x00\x01'+socket.inet_aton('127.0.0.1')+struct.pack('!H',echo.getsockname()[1])+payload
    udp.sendto(packet,relay);reply,_=udp.recvfrom(65535)
    assert reply[:4]==b'\x00\x00\x00\x01' and reply[10:]==payload,'UDP payload or boundary changed'
 finally:
  control.close();stopped.set();thread.join(timeout=2);echo.close()

class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_GET(self):
  self.send_response(200);self.send_header('Content-Length',str(len(BODY)));self.end_headers();self.wfile.write(BODY)
 def do_POST(self):
  body=self.rfile.read(int(self.headers['Content-Length']));assert body==BODY
  self.send_response(200);self.send_header('Content-Length','2');self.end_headers();self.wfile.write(b'OK')
def request(proxy,target,upload=False):
 with proxy_connect(proxy,target) as s:
  head=(f'POST / HTTP/1.0\r\nHost: localhost\r\nContent-Length: {len(BODY)}\r\n\r\n'.encode() if upload else b'GET / HTTP/1.0\r\nHost: localhost\r\n\r\n')
  s.sendall(head+(BODY if upload else b''));result=b''
  while True:
   b=s.recv(65536)
   if not b:break
   result+=b
   if b'\r\n\r\n' in result and len(result.split(b'\r\n\r\n',1)[1]) >= (2 if upload else len(BODY)):break
  header,body=result.split(b'\r\n\r\n',1)
  assert b'200' in header.split(b'\r\n')[0] and body==(b'OK' if upload else BODY)
def verify(binary):
 with tempfile.TemporaryDirectory(prefix='sorvo-bandwidth-test-') as name:
  root=pathlib.Path(name);cert=root/'cert.pem';key=root/'key.pem'
  subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(key),'-out',str(cert),'-days','1','-subj','/CN=localhost'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  fingerprint=hashlib.sha256(ssl.PEM_cert_to_DER_cert(cert.read_text())).hexdigest()
  a,b=str(uuid.uuid4()),str(uuid.uuid4());server_ports=[port(),port()];socks=[port(),port(),port()]
  tls={'certificates':[{'certificateFile':str(cert),'keyFile':str(key)}]}
  server={'log':{'loglevel':'warning'},'inbounds':[], 'outbounds':[{'protocol':'freedom','tag':'direct','settings':{'finalRules':[{'action':'allow','ip':['127.0.0.1/32']}]}}]}
  client={'log':{'loglevel':'warning'},'inbounds':[],'outbounds':[],'routing':{'rules':[]}}
  for i,network in enumerate(['tcp','ws']):
   clients=[{'id':a,'email':'limited','flow':'xtls-rprx-vision' if i==0 else ''},{'id':b,'email':'unlimited','flow':'xtls-rprx-vision' if i==0 else ''}]
   stream={'network':network,'security':'tls','tlsSettings':tls}
   if i:stream['wsSettings']={'path':'/bandwidth-test'}
   server['inbounds'].append({'listen':'127.0.0.1','port':server_ports[i],'protocol':'vless','settings':{'decryption':'none','clients':clients},'streamSettings':stream,'sniffing':{'enabled':True,'destOverride':['http','tls'],'routeOnly':True}})
  for i,(identity,dest) in enumerate([(a,0),(a,1),(b,0)]):
   tag='test-'+str(i);network='tcp' if dest==0 else 'ws'
   client['inbounds'].append({'listen':'127.0.0.1','port':socks[i],'tag':tag,'protocol':'socks','settings':{'auth':'noauth','udp':True}})
   stream={'network':network,'security':'tls','tlsSettings':{'serverName':'localhost','pinnedPeerCertSha256':fingerprint}}
   if dest:stream['wsSettings']={'path':'/bandwidth-test'}
   client['outbounds'].append({'tag':tag,'protocol':'vless','settings':{'vnext':[{'address':'127.0.0.1','port':server_ports[dest],'users':[{'id':identity,'encryption':'none','flow':'xtls-rprx-vision' if dest==0 else ''}]}]},'streamSettings':stream})
   client['routing']['rules'].append({'type':'field','inboundTag':[tag],'outboundTag':tag})
  limits=root/'limits.json';limits.write_text(json.dumps({'limited':{'uploadMbps':2,'downloadMbps':2}}))
  processes=[];logs=[];httpd=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
  threading.Thread(target=httpd.serve_forever,daemon=True).start()
  try:
   for label,config in [('server',server),('client',client)]:
    path=root/(label+'.json');path.write_text(json.dumps(config));log=open(root/(label+'.log'),'w+');logs.append(log)
    env={**os.environ};env.pop('XUI_CLIENT_RATE_LIMITS_FILE',None)
    if label=='server':env['XUI_CLIENT_RATE_LIMITS_FILE']=str(limits)
    processes.append(subprocess.Popen([binary,'run','-c',str(path)],stdout=log,stderr=log,env=env))
   for p in server_ports+socks:
    deadline=time.monotonic()+15
    while True:
     if any(x.poll() is not None for x in processes):raise RuntimeError('core exited before test')
     try:
      with socket.create_connection(('127.0.0.1',p),timeout=.2):break
     except OSError:
      if time.monotonic()>deadline:raise
      time.sleep(.05)
   # Both concurrent sessions use one account, across different transports.
   for upload in [False,True]:
    start=time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(2) as pool:
     futures=[pool.submit(request,p,httpd.server_address[1],upload) for p in socks[:2]]
     for f in futures:f.result()
    elapsed=time.monotonic()-start
    mbps=len(BODY)*2*8/elapsed/1000000
    assert .7<mbps<2.8, f'aggregate cap failed ({mbps:.2f} Mbps)'
    print(('UPLOAD' if upload else 'DOWNLOAD')+f'_AGGREGATE_OK {mbps:.2f} Mbps (configured 2 Mbps; Vision + WS)')
   start=time.monotonic();request(socks[2],httpd.server_address[1]);elapsed=time.monotonic()-start
   assert elapsed<2,'unlimited account was throttled'
   print('UNLIMITED_ACCOUNT_OK')
   for proxy in socks[:2]:verify_udp(proxy)
   print('UDP_PACKET_BOUNDARIES_OK (Vision + WS)')
  except Exception:
   for log in logs:
    log.flush();log.seek(0);print(log.read()[-6000:])
   raise
  finally:
   httpd.shutdown()
   for p in processes:p.terminate()
   for p in processes:
    try:p.wait(timeout=5)
    except subprocess.TimeoutExpired:p.kill();p.wait()
   for log in logs:log.close()
if __name__=='__main__':
 parser=argparse.ArgumentParser();parser.add_argument('binary');args=parser.parse_args();verify(str(pathlib.Path(args.binary).resolve()))
