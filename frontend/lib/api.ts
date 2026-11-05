export class ApiError extends Error { constructor(message:string,public status:number){super(message)} }
export async function api<T>(path:string,options:RequestInit={}):Promise<T>{
  const response=await fetch('/api/v1'+path,{...options,credentials:'same-origin',headers:{...(options.body instanceof FormData?{}:{'Content-Type':'application/json'}),...options.headers},cache:'no-store'});
  const value=await response.json().catch(()=>({error:'Respuesta del servidor inválida'}));
  if(!response.ok) throw new ApiError(value.error||'No se pudo completar la operación',response.status);
  return value as T;
}
export const send=<T>(path:string,body:unknown,method='POST')=>api<T>(path,{method,body:JSON.stringify(body)});
