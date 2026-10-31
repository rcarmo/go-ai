// Reference fixture generator. Source scripts/project-env.sh before running:
// bun scripts/probe-google-sdk-framing.ts <genai-2.21.0.tgz> [--write]
// The archive must match the integrity in upstream 81f3cd41's package-lock.json.
// No package installation, imports or auth code execute. Only the inspected
// HttpResponse, async-generator helpers and ApiClient parser method are evaluated.
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";

const archive = process.argv[2];
if (!archive) throw new Error("Usage: probe-google-sdk-framing.ts <archive> [--write]");
const integrity = "+PDtco2/Z0ONdzCGekCoCT+O1VJS9xJQNN4XzQpXG/t3El/SWWMkCWlFRO1KmivOHPa4Q0VjUYu1HBKCZ/v33Q==";
const bytes = await Bun.file(archive).arrayBuffer();
if (createHash("sha512").update(Buffer.from(bytes)).digest("base64") !== integrity) throw new Error("SDK integrity mismatch");
const extracted = Bun.spawnSync(["tar", "-xOf", archive, "package/dist/node/index.mjs"]);
if (extracted.exitCode !== 0) throw new Error("SDK extraction failed");
const source = extracted.stdout.toString();
function slice(start: string, end: string, from = 0): string {
 const a=source.indexOf(start,from),b=source.indexOf(end,a);
 if(a<0||b<0)throw new Error(`Missing inspected SDK boundary: ${start}`);
 return source.slice(a,b);
}
const helpers=slice("function __await(v)","function __asyncValues");
const response=slice("class HttpResponse {","/** Content filter results");
const client=source.indexOf("class ApiClient {");
if(client<0)throw new Error("Missing ApiClient");
const method=slice("    processStreamResponse(response) {","\n    async apiCall(",client);
const parser=new Function(helpers+response+";return ({"+method+"});")();
const first=JSON.stringify({responseId:"first",candidates:[{content:{parts:[{text:"helloπ🙂"}]}}]});
const stop=JSON.stringify({candidates:[{finishReason:"STOP"}]});
const prefix=`data: ${first}\n\n`,complete=`data: ${stop}\n\n`;
const inputs=[
 ...["\n","\r","\r\n"].map((eol,i)=>({name:["LF","CR","CRLF"][i]+" complete",wire:`data: ${first}${eol}${eol}data: ${stop}${eol}${eol}`})),
 ...["","\n","\r","\r\n"].map((tail,i)=>({name:"EOF tail "+["none","LF","CR","CRLF"][i],wire:prefix+`data: ${stop}`+tail})),
 {name:"whitespace after complete",wire:prefix+complete+" \t\r\n"},
 {name:"unfinished comment after stop",wire:prefix+complete+": heartbeat"},
 {name:"complete comment after stop",wire:prefix+complete+": heartbeat\n\n"},
 {name:"DONE before stop",wire:prefix+"data: [DONE]\n\n"+complete},
 {name:"DONE after stop",wire:prefix+complete+"data: [DONE]\n\n"},
 {name:"mixed LF CRLF boundary",wire:prefix+`data: ${stop}\n\r\n`},
 {name:"event prelude ignored",wire:prefix+`event: message\ndata: ${stop}\n\n`},
 {name:"prefixed multiline JSON",wire:prefix+'data: {\ndata: "candidates":[{"finishReason":"STOP"}]\ndata: }\n\n'},
 {name:"raw multiline JSON",wire:prefix+'data: {\n"candidates":[{"finishReason":"STOP"}]\n}\n\n'},
 {name:"NEL is not SDK whitespace",wire:prefix+complete+'\u0085'},
 {name:"BOM tail is SDK whitespace",wire:prefix+complete+'\ufeff'},
];
async function probe(wire:string,size:number){
 const bytes=new TextEncoder().encode(wire);let offset=0;
 const stream=new ReadableStream<Uint8Array>({pull(controller){if(offset===bytes.length){controller.close();return;}const end=Math.min(offset+size,bytes.length);controller.enqueue(bytes.slice(offset,end));offset=end;}});
 const chunks:unknown[]=[];let failure="";
 try{for await(const item of parser.processStreamResponse(new Response(stream))){chunks.push(await item.json());}}
 catch(error){const message=(error as Error).message;if(message==="Incomplete JSON segment at the end")failure="incomplete";else if(error instanceof SyntaxError)failure="json";else throw error;}
 return {chunks,failure};
}
const cases=[];
for(const input of inputs){const whole=await probe(input.wire,Math.max(1,new TextEncoder().encode(input.wire).length)),split=await probe(input.wire,1);if(JSON.stringify(whole)!==JSON.stringify(split))throw new Error("Read partition mismatch: "+input.name);const terminal=(whole.chunks as any[]).reduce((state,c)=>c.candidates?.[0]?.finishReason||state,"pending");cases.push({...input,...whole,terminal});}
const fixture={upstream:"81f3cd412758ebdd92324c7d63b669a189408d0a",sdk:"@google/genai@2.21.0",integrity:"sha512-"+integrity,reference:"Actual ApiClient.processStreamResponse + HttpResponse.json; whole-buffer and one-byte UTF-8 reads; no SDK object transformations/auth",cases};
if(process.argv.includes("--write")){const output=fileURLToPath(new URL("../inference/provider/google/testdata/sdk_framing_main.json",import.meta.url));await Bun.write(output,JSON.stringify(fixture,null,2)+"\n");}
console.log(JSON.stringify({cases:cases.length,expectedRejections:cases.filter(c=>c.failure).length,accepted:cases.filter(c=>!c.failure).length,partitions:2}));
