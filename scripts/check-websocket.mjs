import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const runtime = JSON.parse(fs.readFileSync(path.join(root,'.cache/check-runtime.json')));
const lan = JSON.parse(fs.readFileSync(path.join(root,'data/tls/addresses.json'))).find(a=>a.startsWith('192.168.'));
async function api(route,body,token=runtime.token,base='http://127.0.0.1:8766') {
  const res = await fetch(base+route,{method:body===undefined?'GET':'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
  if(!res.ok) throw new Error(`${res.status}: ${await res.text()}`);
  return res.json();
}
const pair = await api('/v1/pair/start',{});
const device = await api('/v1/pair/claim',{code:pair.code,kind:'web',name:'WSS voice check'},'',`https://${lan}:8765`);
const session = await api('/v1/sessions',{},device.token,`https://${lan}:8765`);
const sockets = ['control','data'].map(plane=>new WebSocket(`wss://${lan}:8765/v1/${plane}?session=${session.id}&token=${device.token}`));
const reports=[];
try {
  await Promise.all(sockets.map(ws=>new Promise((resolve,reject)=>{ws.addEventListener('open',resolve,{once:true});ws.addEventListener('error',reject,{once:true});})));
  async function turn(send,label,wantTranscript=false) {
    return new Promise((resolve,reject)=>{
      const result={label,audio_chunks:0,sample_rate:0,text:'',transcript:'',provider:''};
      let done=false;
      const started=Date.now();
      const timeout=setTimeout(()=>finish(new Error(`Timed out: ${JSON.stringify(result)}`)),180000);
      function finish(error) { clearTimeout(timeout);sockets.forEach(ws=>ws.removeEventListener('message',receive)); if(error)reject(error);else resolve({...result,seconds:(Date.now()-started)/1000}); }
      function receive(event) {
        const frame=JSON.parse(event.data);const p=frame.payload??{};
        if(frame.type==='error') return finish(new Error(JSON.stringify(p)));
        if(frame.type==='turn.delta') result.text+=p.text??'';
        if(frame.type==='transcript.final') result.transcript=p.text;
        if(frame.type==='turn.done'){done=true;result.provider=p.provider;}
        if(frame.type==='tts.chunk'){result.audio_chunks++;result.sample_rate=p.sample_rate;}
        if(done&&result.audio_chunks>0&&(!wantTranscript||result.transcript))finish();
      }
      sockets.forEach(ws=>ws.addEventListener('message',receive));
      send();
    });
  }
  for(let i=0;i<2;i++){
    const result=await turn(()=>sockets[1].send(JSON.stringify({type:'text',text:'Ответь одним словом: сколько будет два плюс два?'})),`gpu-turn-${i+1}`);
    if(result.provider!=='qwen35-9b-gpu')throw new Error(`Warm GPU selection failed: ${JSON.stringify(result)}`);
    reports.push(result);console.log(JSON.stringify(result));
  }
  const wav=fs.readFileSync(path.join(root,'artifacts/yui-voice-check.wav'));
  let offset=12,pcm,rate;
  while(offset+8<=wav.length){const name=wav.toString('ascii',offset,offset+4),size=wav.readUInt32LE(offset+4);if(name==='fmt ')rate=wav.readUInt32LE(offset+12);if(name==='data')pcm=wav.subarray(offset+8,offset+8+size);offset+=8+size+(size%2);}
  const voice=await turn(()=>{sockets[1].send(pcm);sockets[1].send(JSON.stringify({type:'audio.end',sample_rate:rate,channels:1}));},'voice-roundtrip',true);
  reports.push(voice);console.log(JSON.stringify(voice));
  await api(`/v1/sessions/${session.id}/provider`,{kind:'llm',provider_id:'qwen35-4b-cpu'});
  const cpu=await turn(()=>sockets[1].send(JSON.stringify({type:'text',text:'Ответь одним словом: сколько будет два плюс два?'})),'4b-cpu');
  if(cpu.provider!=='qwen35-4b-cpu')throw new Error('CPU lock was not applied');
  reports.push(cpu);console.log(JSON.stringify(cpu));
  fs.writeFileSync(path.join(root,'artifacts/websocket-check.json'),JSON.stringify(reports,null,2));
} finally {
  sockets.forEach(ws=>ws.close());
  await api(`/v1/sessions/${session.id}/close`,{});
  await api(`/v1/devices/${device.device_id}/revoke`,{});
}
