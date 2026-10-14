# Encodes an OpenRGB controller description at SDK protocol version 1, as the
# server sends it for REQUEST_CONTROLLER_DATA. Explicit little-endian, no
# padding.
import struct, sys
def s(t): b=t.encode()+b"\0"; return struct.pack("<H",len(b))+b
def col(r,g,b): return struct.pack("<BBBx",r,g,b)
def mode(name,value,flags,cmode,colors=(),speed=(0,0,0),cmm=(0,0),direction=0):
    d=s(name)+struct.pack("<iI",value,flags)+struct.pack("<II",speed[0],speed[1])
    d+=struct.pack("<II",*cmm)+struct.pack("<I",speed[2])+struct.pack("<II",direction,cmode)
    d+=struct.pack("<H",len(colors))+b"".join(col(*c) for c in colors)
    return d
N=0xFFFFFFFF
matrix=[[0,1,2,N],[3,4,5,6],[7,N,8,9]]
modes=[mode("Direct",0,1<<5,1),
       mode("Static",1,1<<6,2,[(255,0,0)],cmm=(1,1)),
       mode("Rainbow Wave",2,(1<<0)|(1<<1),0,speed=(1,10,5),direction=1)]
zones=[s("Keyboard")+struct.pack("<iIII",2,10,10,10)+struct.pack("<H",(3*4+2)*4)+struct.pack("<II",3,4)+b"".join(struct.pack("<I",v) for r in matrix for v in r),
       s("Logo")+struct.pack("<iIII",0,1,1,1)+struct.pack("<H",0)]
names=["Key: Esc","Key: 1","Key: 2","Key: Q","Key: W","Key: E","Key: R","Key: Left Control","Key: Space","Key: Right Control","Logo"]
colors=[(i*10,i*20%256,255-i) for i in range(11)]
body=struct.pack("<i",5)+s("Test Keyboard")+s("Acme")+s("A test keyboard")+s("1.0")+s("SN1")+s("HID: /dev/hidraw0")
body+=struct.pack("<H",len(modes))+struct.pack("<i",2)+b"".join(modes)
body+=struct.pack("<H",len(zones))+b"".join(zones)
body+=struct.pack("<H",len(names))+b"".join(s(n)+struct.pack("<I",0) for n in names)
body+=struct.pack("<H",len(colors))+b"".join(col(*c) for c in colors)
data=struct.pack("<I",len(body)+4)+body
open(sys.argv[1],"wb").write(data)
open(sys.argv[2],"wb").write(modes[2])  # raw "Rainbow Wave" mode, as UPDATEMODE carries it
