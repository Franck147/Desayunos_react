import type { Metadata } from 'next';
import './globals.css';
export const metadata: Metadata = {title:'Desayunos · Hecho para tu mañana',description:'Tu desayuno favorito, recién preparado. Pide, recoge y disfruta.',icons:{icon:'/favicon.svg'}};
export default function RootLayout({children}:Readonly<{children:React.ReactNode}>){return <html lang="es-PE"><body>{children}</body></html>}
