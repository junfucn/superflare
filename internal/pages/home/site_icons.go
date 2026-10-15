package home

const _inlineSiteIconRefreshScript = `(function(){
var repairKeyPrefix="superflare.site-icon.primary-favicon:";
var directNodes=Array.prototype.slice.call(document.querySelectorAll("img[data-site-icon-direct][data-site-icon-fallback-src]"));
function needsDirectRepair(img){if(!img.hasAttribute("data-site-icon-direct")){return false;}var src=img.getAttribute("src");if(!src){return false;}try{return window.localStorage.getItem(repairKeyPrefix+src)!=="1";}catch(error){return true;}}
function markSourceRepaired(src){try{window.localStorage.setItem(repairKeyPrefix+src,"1");}catch(error){}}
directNodes.forEach(function(img){function useFallback(){var fallback=img.dataset.siteIconFallbackSrc;if(!fallback||img.dataset.siteIconFallbackDone==="1"){return;}img.dataset.siteIconFallbackDone="1";img.removeAttribute("data-site-icon-direct");img.src=fallback;}img.addEventListener("error",useFallback,{once:true});if(img.complete&&img.naturalWidth===0){useFallback();}});
var nodes=Array.prototype.slice.call(document.querySelectorAll("[data-site-icon-src]"));
directNodes.forEach(function(img){if(needsDirectRepair(img)){nodes.push(img);}});
if(!nodes.length||!window.fetch||!window.URL||!URL.createObjectURL){return;}
var groups=new Map();
nodes.forEach(function(node){var src=node.dataset.siteIconSrc||node.getAttribute("src");if(!src){return;}var group=groups.get(src);if(group){group.push(node);}else{groups.set(src,[node]);}});
var objectUrls=[];
var hidden=false;
function displayIcon(group,src,blob){
if(!blob||hidden){return;}
var objectUrl=URL.createObjectURL(blob);var probe=new Image();var settled=false;
function apply(){if(settled){return;}if(hidden){discard();return;}settled=true;objectUrls.push(objectUrl);group.forEach(function(node){if(node.tagName==="IMG"){node.src=objectUrl;node.dataset.siteIconDone="1";return;}var img=document.createElement("img");img.src=objectUrl;img.alt="";img.dataset.siteIconDone="1";node.replaceWith(img);});markSourceRepaired(src);}
function discard(){if(settled){return;}settled=true;URL.revokeObjectURL(objectUrl);}
if(typeof probe.decode==="function"){probe.src=objectUrl;probe.decode().then(apply,discard);}else{probe.onload=apply;probe.onerror=discard;probe.src=objectUrl;}
}
function loadDirect(group,src){
fetch(src,{cache:"reload"}).then(function(res){if(!res.ok||res.headers.get("X-SuperFlare-Site-Icon")!=="cached"){return null;}return res.blob();}).then(function(blob){displayIcon(group,src,blob);}).catch(function(){});
}
function loadStream(entries){
var pending=new Map(entries);
function applyRecord(line){
try{
var record=JSON.parse(line);var item=pending.get(record.source);if(!item){return;}
if(!record.data||!record.contentType||record.contentType.indexOf("image/")!==0){pending.delete(record.source);loadDirect(item.group,item.src);return;}
pending.delete(record.source);
var binary=atob(record.data);var bytes=new Uint8Array(binary.length);
for(var i=0;i<binary.length;i++){bytes[i]=binary.charCodeAt(i);}
displayIcon(item.group,item.src,new Blob([bytes],{type:record.contentType}));
}catch(error){}
}
fetch("/assets/site-icons/stream",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sources:Array.from(pending.keys())})}).then(async function(res){
if(!res.ok||!res.body||!res.body.getReader){throw new Error("Icon stream unavailable");}
var reader=res.body.getReader();var decoder=new TextDecoder();var buffer="";
try{
while(true){
var chunk=await reader.read();buffer+=decoder.decode(chunk.value||new Uint8Array(),{stream:!chunk.done});
var lines=buffer.split("\n");buffer=lines.pop();lines.forEach(applyRecord);
if(chunk.done){if(buffer.trim()){applyRecord(buffer);}break;}
}
}finally{reader.releaseLock();}
if(pending.size){throw new Error("Incomplete icon stream");}
}).catch(function(){if(!hidden){pending.forEach(function(item){loadDirect(item.group,item.src);});}});
}
var streaming=[];
groups.forEach(function(group,src){
if(window.ReadableStream&&window.TextDecoder&&group.some(function(node){return node.hasAttribute("data-site-icon-src");})){
try{
var proxyURL=new URL(src,window.location.href);var source=proxyURL.searchParams.get("src");
if(proxyURL.origin===window.location.origin&&proxyURL.pathname==="/assets/site-icons"&&source&&/^https?:\/\//i.test(source)){
streaming.push([source,{group:group,src:src}]);return;
}
}catch(error){}
}
loadDirect(group,src);
});
for(var offset=0;offset<streaming.length;offset+=128){loadStream(streaming.slice(offset,offset+128));}
window.addEventListener("pagehide",function(event){if(event.persisted){return;}hidden=true;objectUrls.forEach(function(objectUrl){URL.revokeObjectURL(objectUrl);});});
}());`
