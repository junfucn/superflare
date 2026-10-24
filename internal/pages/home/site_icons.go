package home

const _inlineSiteIconRefreshScript = `(function(){
function refreshSiteIcons(){
var repairKeyPrefix="superflare.site-icon.primary-favicon:";
var directNodes=Array.prototype.slice.call(document.querySelectorAll("img[data-site-icon-direct][data-site-icon-fallback-src]"));
function needsDirectRepair(img){if(!img.hasAttribute("data-site-icon-direct")){return false;}var src=img.getAttribute("src");if(!src){return false;}try{return window.localStorage.getItem(repairKeyPrefix+src)!=="1";}catch(error){return true;}}
function markSourceRepaired(src){try{window.localStorage.setItem(repairKeyPrefix+src,"1");}catch(error){}}
directNodes.forEach(function(img){function useFallback(){var fallback=img.dataset.siteIconFallbackSrc;if(!fallback||img.dataset.siteIconFallbackDone==="1"){return;}img.dataset.siteIconFallbackDone="1";img.removeAttribute("data-site-icon-direct");img.src=fallback;}img.addEventListener("error",useFallback,{once:true});if(img.complete&&img.naturalWidth===0){useFallback();}});
var nodes=Array.prototype.slice.call(document.querySelectorAll("[data-site-icon-src]"));
directNodes.forEach(function(img){if(needsDirectRepair(img)){nodes.push(img);}});
var submenuImages=Array.prototype.slice.call(document.querySelectorAll(".application-subdirectory-modal img,details img"));
if((!nodes.length&&!submenuImages.length)||!window.fetch||!window.URL||!URL.createObjectURL){return;}
var groups=new Map();
nodes.forEach(function(node){var src=node.dataset.siteIconSrc||node.getAttribute("src");if(!src){return;}var group=groups.get(src);if(group){group.push(node);}else{groups.set(src,[node]);}});
var objectUrls=[];
var hidden=false;
var requestController=window.AbortController?new window.AbortController():null;
function displayIcon(group,src,blob){
if(!blob||hidden){return Promise.resolve();}
return new Promise(function(resolve){
var objectUrl=URL.createObjectURL(blob);var probe=new Image();var settled=false;
function apply(){if(settled){return;}if(hidden){discard();return;}settled=true;objectUrls.push(objectUrl);group.forEach(function(node){if(node.tagName==="IMG"){if(isSubmenu(node)){node.loading="eager";}node.src=objectUrl;node.dataset.siteIconDone="1";return;}var img=document.createElement("img");img.loading="lazy";img.decoding="async";img.src=objectUrl;if(isSubmenu(node)){img.loading="eager";}img.alt="";img.dataset.siteIconDone="1";node.replaceWith(img);});markSourceRepaired(src);resolve();}
function discard(){if(settled){return;}settled=true;URL.revokeObjectURL(objectUrl);resolve();}
if(typeof probe.decode==="function"){probe.src=objectUrl;probe.decode().then(apply,discard);}else{probe.onload=apply;probe.onerror=discard;probe.src=objectUrl;}
});
}
function loadDirect(group,src,background){
return fetch(src,{cache:"reload",signal:requestController?requestController.signal:undefined,priority:background?"low":"auto"}).then(function(res){if(!res.ok||res.headers.get("X-SuperFlare-Site-Icon")!=="cached"){return null;}return res.blob();}).then(function(blob){return displayIcon(group,src,blob);}).catch(function(){});
}
function loadStream(entries,background){
var pending=new Map(entries);
var recovery=[];
function applyRecord(line){
try{
var record=JSON.parse(line);var item=pending.get(record.source);if(!item){return;}
if(!record.data||!record.contentType||record.contentType.indexOf("image/")!==0){pending.delete(record.source);recovery.push(loadDirect(item.group,item.src,background));return;}
pending.delete(record.source);
var binary=atob(record.data);var bytes=new Uint8Array(binary.length);
for(var i=0;i<binary.length;i++){bytes[i]=binary.charCodeAt(i);}
recovery.push(displayIcon(item.group,item.src,new Blob([bytes],{type:record.contentType})));
}catch(error){}
}
return fetch("/assets/site-icons/stream",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sources:Array.from(pending.keys())}),signal:requestController?requestController.signal:undefined,priority:background?"low":"auto"}).then(async function(res){
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
}).catch(function(){if(!hidden){pending.forEach(function(item){recovery.push(loadDirect(item.group,item.src,background));});}}).then(function(){return Promise.all(recovery);});
}
function loadGroups(entries,background){
var streaming=[];
var requests=[];
entries.forEach(function(entry){
var src=entry[0];var group=entry[1];
if(window.ReadableStream&&window.TextDecoder&&group.some(function(node){return node.hasAttribute("data-site-icon-src");})){
try{
var proxyURL=new URL(src,window.location.href);var source=proxyURL.searchParams.get("src");
if(proxyURL.origin===window.location.origin&&proxyURL.pathname==="/assets/site-icons"&&source&&/^https?:\/\//i.test(source)){
streaming.push([source,{group:group,src:src}]);return;
}
}catch(error){}
}
requests.push(loadDirect(group,src,background));
});
for(var offset=0;offset<streaming.length;offset+=128){requests.push(loadStream(streaming.slice(offset,offset+128),background));}
return Promise.all(requests);
}
var waiting=new Map(groups);var ready=new Map();var sourceByNode=new Map();var observer=null;var flushScheduled=false;
// Visible requests and image decoding take precedence over submenu prefetch.
var foregroundLoads=0;var trackedImages=new Set();var preloadScheduled=false;var preloading=false;
function isSubmenu(node){return node.closest("details")||node.closest(".application-subdirectory-modal");}
function isAvailable(node){
if(!node.isConnected||node.closest("details:not([open])")){return false;}
var modal=node.closest(".application-subdirectory-modal");
return !modal||window.location.hash==="#"+modal.id;
}
function nearViewport(node){
if(!isAvailable(node)){return false;}
var rect=node.getBoundingClientRect();
return rect.width>0&&rect.height>0&&rect.bottom>=-160&&rect.top<=window.innerHeight+160&&rect.right>=-160&&rect.left<=window.innerWidth+160;
}
function queueSource(src){
var group=waiting.get(src);if(!group||hidden){return;}
waiting.delete(src);ready.set(src,group);
if(observer){group.forEach(function(node){observer.unobserve(node);});}
if(flushScheduled){return;}flushScheduled=true;
Promise.resolve().then(function(){flushScheduled=false;if(hidden){return;}var entries=Array.from(ready.entries());ready.clear();foregroundLoads++;loadGroups(entries).then(finishForeground,finishForeground);});
}
function finishForeground(){foregroundLoads--;schedulePreload();}
function trackVisibleImages(){
Array.prototype.slice.call(document.querySelectorAll("img")).forEach(function(img){
if(img.complete||trackedImages.has(img)||!nearViewport(img)){return;}
trackedImages.add(img);foregroundLoads++;
var settled=false;
function finish(){if(settled){return;}settled=true;img.removeEventListener("load",finish);img.removeEventListener("error",finish);finishForeground();}
img.addEventListener("load",finish,{once:true});img.addEventListener("error",finish,{once:true});
if(img.complete){finish();}
});
}
function schedulePreload(){
if(hidden||preloadScheduled||preloading||foregroundLoads||flushScheduled||ready.size){return;}
if(!submenuImages.length&&!Array.from(waiting.values()).some(function(group){return group.some(isSubmenu);})){return;}
preloadScheduled=true;
function run(){preloadScheduled=false;if(hidden||preloading||foregroundLoads||flushScheduled||ready.size){return;}preloadSubmenus();}
if(window.requestIdleCallback){window.requestIdleCallback(run,{timeout:1000});}else{window.setTimeout(run,0);}
}
function preloadSubmenus(){
var entries=[];
waiting.forEach(function(group,src){
if(entries.length>=4||!group.some(isSubmenu)){return;}
waiting.delete(src);if(observer){group.forEach(function(node){observer.unobserve(node);});}entries.push([src,group]);
});
submenuImages.forEach(function(img){if(img.isConnected&&!sourceByNode.has(img)){img.fetchPriority="low";img.loading="eager";}});
submenuImages=[];
if(!entries.length){return;}
preloading=true;
function finish(){preloading=false;schedulePreload();}
loadGroups(entries,true).then(finish,finish);
}
function scanVisible(){
if(hidden){return;}
trackVisibleImages();
waiting.forEach(function(group,src){if(group.some(nearViewport)){queueSource(src);}});
schedulePreload();
}
var scanScheduled=false;
function scheduleScan(){
if(scanScheduled||hidden){return;}scanScheduled=true;
window.requestAnimationFrame(function(){scanScheduled=false;scanVisible();});
}
if(window.IntersectionObserver){
observer=new window.IntersectionObserver(function(entries){entries.forEach(function(entry){if(entry.isIntersecting&&isAvailable(entry.target)){queueSource(sourceByNode.get(entry.target));}});},{rootMargin:"160px"});
}
groups.forEach(function(group,src){
group.forEach(function(node){sourceByNode.set(node,src);if(observer){observer.observe(node);}});
});
scanVisible();
window.addEventListener("scroll",scheduleScan,{passive:true,capture:true});window.addEventListener("resize",scheduleScan,{passive:true});
document.addEventListener("toggle",scheduleScan,true);
window.addEventListener("hashchange",scheduleScan);
window.addEventListener("pageshow",scheduleScan);
window.addEventListener("pagehide",function(event){if(event.persisted){return;}hidden=true;if(requestController){requestController.abort();}if(observer){observer.disconnect();}objectUrls.forEach(function(objectUrl){URL.revokeObjectURL(objectUrl);});});
}
function scheduleRefresh(){window.setTimeout(refreshSiteIcons,0);}
if(document.readyState==="complete"){scheduleRefresh();}else{window.addEventListener("load",scheduleRefresh,{once:true});}
}());`
