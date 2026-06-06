import { useId } from 'react'

export default function Input({ label, error, className = '', id, ...props }) {
  // Associate the label with the input so assistive tech (and getByLabelText)
  // can resolve the control. Respect a caller-supplied id, else generate one.
  const generatedId = useId()
  const inputId = id || generatedId
  return (
    <div className="space-y-1">
      {label && (
        <label htmlFor={inputId} className="block text-sm font-medium text-gray-700">{label}</label>
      )}
      <input
        id={inputId}
        className={`block w-full rounded-lg border px-3 py-2 text-sm placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition ${
          error ? 'border-red-500 bg-red-50' : 'border-gray-300 bg-white'
        } ${className}`}
        {...props}
      />
      {error && <p className="text-xs text-red-600">{error}</p>}
    </div>
  )
}
